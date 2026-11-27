package lucene

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type parityDoc struct {
	ID     string         `json:"id"`
	Status *string        `json:"status"`
	Title  string         `json:"title"`
	Year   *int           `json:"year"`
	Tags   []string       `json:"tags"`
	Meta   map[string]any `json:"meta"`
}

// parityFixture is the same four documents in both stores.
//
//	id | status | title     | year | tags              | meta
//	 1 | open   | Room 12   | 2021 | [golang, gopher]  | {k: a}
//	 2 | closed | roam      | 2024 | [rust]            | {k: b}
//	 3 | -      | Boardroom | 2019 | []                | {}
//	 4 | open   | kitchen   | -    | -                 | {k: a}
//	 5 | open   | ТРЪБА PVC Ф 63 | 2022 | -            | -
var parityFixture = []map[string]any{
	{"id": "1", "status": "open", "title": "Room 12", "year": 2021, "tags": []string{"golang", "gopher"}, "meta": map[string]any{"k": "a"}},
	{"id": "2", "status": "closed", "title": "roam", "year": 2024, "tags": []string{"rust"}, "meta": map[string]any{"k": "b"}},
	{"id": "3", "title": "Boardroom", "year": 2019, "tags": []string{}, "meta": map[string]any{}},
	{"id": "4", "status": "open", "title": "kitchen", "meta": map[string]any{"k": "a"}},
	{"id": "5", "status": "open", "title": "ТРЪБА PVC Ф 63", "year": 2022},
}

const parityMapping = `{
"settings":{"analysis":{"normalizer":{"lc":{"type":"custom","filter":["lowercase"]}}}},
"mappings":{"properties":{
	"id":{"type":"keyword"},
	"status":{"type":"keyword"},
	"title":{"type":"keyword","fields":{
		"lc":{"type":"keyword","normalizer":"lc"},
		"text":{"type":"text","analyzer":"bulgarian"}}},
	"year":{"type":"integer"},
	"tags":{"type":"keyword"},
	"meta":{"properties":{"k":{"type":"keyword"}}}
}}}`

// parityQueries are the queries of the issue table, then the hostile inputs
// of TestElasticsearchUserTextStaysAValue.
var parityQueries = []string{
	`status:open`,
	`status:null`,
	`NOT status:null`,
	`NOT status:closed`,
	`-status:closed`,
	`status:(open OR null)`,
	`title:room*`,
	`title:ro?m*`,
	`room`,
	`year:[2020 TO 2024]`,
	`year:{2019 TO 2024}`,
	`year:[* TO 2020]`,
	`NOT year:2021`,
	`tags:golang`,
	`NOT tags:golang`,
	`tags:go*`,
	`meta.k:a`,
	`status:open AND year:[2020 TO *]`,

	`tags:*`,
	`tags:Golang`,

	// Elasticsearch refuses a query nested deeper than 30 levels.
	strings.Repeat(`status:open AND `, 39) + `status:open`,
	strings.Repeat(`status:open OR `, 39) + `status:open`,
	strings.TrimSpace(strings.Repeat(`room `, 35)),

	`title:"x\"}},{\"script\":{\"source\":\"1\"}}"`,
	`title:query_string*`,
	`title:"a\\b"`,
	`title:*`,
	`script`,
}

// parityDivergence holds the search-server ids where they knowingly differ
// from SQLite. SQL matches tags:* on an empty array (IS NOT NULL); a search
// server indexes [] as no value, so exists skips document 3.
// parityWithPattern runs with Pattern set on title. SQLite's LIKE folds
// ASCII only, so the ids are pinned: PostgreSQL's ILIKE matches the same.
var parityWithPattern = map[string][]string{
	`title:*тръба*`: {"5"},
	`title:*ТРЪБА*`: {"5"},
	`title:тръба*`:  {"5"},
	`title:*room*`:  {"1", "3"},
	`tags:Golang`:   {},
}

// parityWithText runs with Text set on title: ranked full text with the
// bulgarian analyzer. It finds typos and other word forms, which SQL does not,
// so the ids are pinned.
var parityWithText = map[string][]string{
	`тръби`:             {"5"},
	`тръвба`:            {"5"},
	`room`:              {"1", "2"}, // roam is one edit away: fuzziness AUTO
	`тръби -pvc`:        {},
	`status:open тръби`: {"5"},
}

var parityDivergence = map[string][]string{
	`tags:*`: {"1", "2"},
}

// TestElasticsearchParity runs each query on SQLite through ParseToSQL and on
// each search server through ParseToElasticsearch, and compares the ids.
//
// It needs a server: set ELASTICSEARCH_URL and/or OPENSEARCH_URL. CI also sets
// MAGIC_REQUIRE_SEARCH_SERVERS=1, so a missing URL fails there instead of
// skipping, and the check can never pass by checking nothing.
func TestElasticsearchParity(t *testing.T) {
	servers := map[string]string{
		"elasticsearch": os.Getenv("ELASTICSEARCH_URL"),
		"opensearch":    os.Getenv("OPENSEARCH_URL"),
	}
	require := os.Getenv("MAGIC_REQUIRE_SEARCH_SERVERS") == "1"

	p, err := NewParser(parityDoc{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	db := newParityDB(t)
	want := map[string][]string{}
	for _, q := range parityQueries {
		want[q] = sqliteIDs(t, p, db, q)
	}

	ran := false
	for name, url := range servers {
		if url == "" {
			if require {
				t.Errorf("%s: no URL set, and MAGIC_REQUIRE_SEARCH_SERVERS=1", name)
			}
			continue
		}
		ran = true
		t.Run(name, func(t *testing.T) {
			index := newParityIndex(t, strings.TrimRight(url, "/"))
			t.Run("with Text", func(t *testing.T) {
				opt := WithElasticsearchFields(map[string]ElasticsearchField{"title": {Pattern: "title.lc", Text: "title.text"}})
				for q, expected := range parityWithText {
					if got := index.ids(t, p, q, opt); !reflect.DeepEqual(got, expected) {
						t.Errorf("%s %s matched %v, want %v", name, q, got, expected)
					}
				}
				// Ranked: the exact word comes before the one-edit match.
				if got := index.ranked(t, p, `roam`, opt); len(got) != 2 || got[0] != "2" {
					t.Errorf("%s roam ranked %v, want [2 1]", name, got)
				}
			})
			t.Run("with Pattern", func(t *testing.T) {
				opt := WithElasticsearchFields(map[string]ElasticsearchField{"title": {Pattern: "title.lc"}})
				for q, expected := range parityWithPattern {
					if got := index.ids(t, p, q, opt); !reflect.DeepEqual(got, expected) {
						t.Errorf("%s %s matched %v, want %v", name, q, got, expected)
					}
				}
			})
			for _, q := range parityQueries {
				t.Run(q, func(t *testing.T) {
					expected, diverges := parityDivergence[q]
					if !diverges {
						expected = want[q]
					}
					got := index.ids(t, p, q)
					if !reflect.DeepEqual(got, expected) {
						t.Errorf("%s matched %v, want %v (SQLite matched %v)", name, got, expected, want[q])
					}
				})
			}
		})
	}
	if !ran && !require {
		t.Skip("set ELASTICSEARCH_URL and/or OPENSEARCH_URL to run the parity test")
	}
}

func newParityDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE docs (id TEXT, status TEXT, title TEXT, year INTEGER, tags TEXT, meta TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, doc := range parityFixture {
		var tags, meta any
		if v, ok := doc["tags"]; ok {
			b, _ := json.Marshal(v)
			tags = string(b)
		}
		if v, ok := doc["meta"]; ok {
			b, _ := json.Marshal(v)
			meta = string(b)
		}
		if _, err := db.Exec(`INSERT INTO docs VALUES (?, ?, ?, ?, ?, ?)`,
			doc["id"], doc["status"], doc["title"], doc["year"], tags, meta); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return db
}

func sqliteIDs(t *testing.T, p *Parser, db *sql.DB, q string) []string {
	t.Helper()
	where, params, err := p.ParseToSQL(q, "sqlite")
	if err != nil {
		t.Fatalf("ParseToSQL(%q): %v", q, err)
	}
	rows, err := db.Query("SELECT id FROM docs WHERE "+where, params...)
	if err != nil {
		t.Fatalf("SQLite query for %q: %v", q, err)
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(ids)
	return ids
}

type parityIndex struct {
	url string
}

// newParityIndex creates a fresh index on the server, loads the fixture and
// deletes the index when the test ends.
func newParityIndex(t *testing.T, server string) parityIndex {
	t.Helper()
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	idx := parityIndex{url: server + "/magic-parity-" + hex.EncodeToString(suffix)}

	esCall(t, http.MethodPut, idx.url, "application/json", parityMapping)
	t.Cleanup(func() { esCall(t, http.MethodDelete, idx.url, "", "") })

	var bulk strings.Builder
	for _, doc := range parityFixture {
		fmt.Fprintf(&bulk, `{"index":{"_id":%q}}`+"\n", doc["id"])
		b, _ := json.Marshal(doc)
		bulk.Write(b)
		bulk.WriteString("\n")
	}
	var res struct {
		Errors bool `json:"errors"`
	}
	if err := json.Unmarshal(esCall(t, http.MethodPost, idx.url+"/_bulk?refresh=true", "application/x-ndjson", bulk.String()), &res); err != nil || res.Errors {
		t.Fatalf("bulk load failed: %v", err)
	}
	return idx
}

// ranked returns the ids in the order the server scores them.
func (idx parityIndex) ranked(t *testing.T, p *Parser, q string, opts ...ElasticsearchOption) []string {
	t.Helper()
	dsl, err := p.ParseToElasticsearch(q, opts...)
	if err != nil {
		t.Fatalf("ParseToElasticsearch(%q): %v", q, err)
	}
	body, _ := json.Marshal(map[string]any{"query": dsl, "size": 100, "_source": false})
	var res struct {
		Hits struct {
			Hits []struct {
				ID string `json:"_id"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(esCall(t, http.MethodPost, idx.url+"/_search", "application/json", string(body)), &res); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	ids := []string{}
	for _, h := range res.Hits.Hits {
		ids = append(ids, h.ID)
	}
	return ids
}

func (idx parityIndex) ids(t *testing.T, p *Parser, q string, opts ...ElasticsearchOption) []string {
	t.Helper()
	ids := idx.ranked(t, p, q, opts...)
	slices.Sort(ids)
	return ids
}

// esCall sends one request and fails the test on any status other than 2xx,
// quoting the server's answer.
func esCall(t *testing.T, method, url, contentType, body string) []byte {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("%s %s: %s: %s", method, url, resp.Status, out)
	}
	return out
}

// TestGoModHasNoSearchClient keeps the renderer free of a client dependency:
// the caller picks the client and the server version.
func TestGoModHasNoSearchClient(t *testing.T) {
	mod, err := os.ReadFile("../../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, dep := range []string{"elastic/go-elasticsearch", "opensearch-project/opensearch-go", "olivere/elastic"} {
		if bytes.Contains(mod, []byte(dep)) {
			t.Errorf("go.mod requires %s; the renderer must stay client-free", dep)
		}
	}
}
