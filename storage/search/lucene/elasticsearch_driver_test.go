package lucene

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type esDoc struct {
	ID     string         `json:"id"`
	Status *string        `json:"status"`
	Title  string         `json:"title"`
	Year   int            `json:"year"`
	Tags   []string       `json:"tags"`
	Nums   []int          `json:"nums"`
	Meta   map[string]any `json:"meta"`
}

func newESParser(t *testing.T) *Parser {
	t.Helper()
	p, err := NewParser(esDoc{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	return p
}

// jsonValue normalises v through a JSON round trip, so a rendered map and a
// literal JSON string compare equal when they serialise to the same document.
func jsonValue(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestElasticsearchGolden(t *testing.T) {
	p := newESParser(t)
	notClosed := `{"bool":{"must_not":[{"term":{"status":{"value":"closed"}}}]}}`
	tests := []struct {
		query string
		want  string
	}{
		{``, `{"match_all":{}}`},
		{`status:open`, `{"term":{"status":{"value":"open"}}}`},
		{`+status:open`, `{"term":{"status":{"value":"open"}}}`},
		{`year:2021`, `{"term":{"year":{"value":2021}}}`},
		{`status:null`, `{"bool":{"must_not":[{"exists":{"field":"status"}}]}}`},
		{`NOT status:null`, `{"exists":{"field":"status"}}`},
		{`-status:null`, `{"exists":{"field":"status"}}`},
		{`NOT status:closed`, notClosed},
		{`-status:closed`, notClosed},
		{`status:open AND year:2021`, `{"bool":{"filter":[{"term":{"status":{"value":"open"}}},{"term":{"year":{"value":2021}}}]}}`},
		{`status:(open OR null)`, `{"bool":{"should":[{"term":{"status":{"value":"open"}}},{"bool":{"must_not":[{"exists":{"field":"status"}}]}}],"minimum_should_match":1}}`},
		{`status:(open AND closed)`, `{"bool":{"filter":[{"term":{"status":{"value":"open"}}},{"term":{"status":{"value":"closed"}}}]}}`},
		{`tags:(go OR rust)`, `{"bool":{"should":[{"term":{"tags":{"value":"go"}}},{"term":{"tags":{"value":"rust"}}}],"minimum_should_match":1}}`},
		{`title:room*`, `{"wildcard":{"title":{"value":"room*","case_insensitive":true}}}`},
		{`title:ro?m*`, `{"wildcard":{"title":{"value":"ro?m*","case_insensitive":true}}}`},
		{`room`, `{"bool":{"should":[{"wildcard":{"id":{"value":"*room*","case_insensitive":true}}},{"wildcard":{"title":{"value":"*room*","case_insensitive":true}}}],"minimum_should_match":1}}`},
		{`tags:go*`, `{"wildcard":{"tags":{"value":"go*","case_insensitive":true}}}`},
		{`title:*`, `{"exists":{"field":"title"}}`},
		{`year:*`, `{"exists":{"field":"year"}}`},
		{`year:[2020 TO 2024]`, `{"range":{"year":{"gte":2020,"lte":2024}}}`},
		{`year:{2019 TO 2024}`, `{"range":{"year":{"gt":2019,"lt":2024}}}`},
		{`year:[* TO 2020]`, `{"range":{"year":{"lte":2020}}}`},
		{`year:{2019 TO *}`, `{"range":{"year":{"gt":2019}}}`},
		{`year:>2020`, `{"range":{"year":{"gt":2020}}}`},
		{`year:>=2020`, `{"range":{"year":{"gte":2020}}}`},
		{`year:<2020`, `{"range":{"year":{"lt":2020}}}`},
		{`year:<=2020`, `{"range":{"year":{"lte":2020}}}`},
		{`tags:golang`, `{"term":{"tags":{"value":"golang"}}}`},
		{`NOT tags:golang`, `{"bool":{"must_not":[{"term":{"tags":{"value":"golang"}}}]}}`},
		{`nums:5`, `{"term":{"nums":{"value":5}}}`},
		{`tags:null`, `{"bool":{"must_not":[{"exists":{"field":"tags"}}]}}`},
		{`meta.k:a`, `{"term":{"meta.k":{"value":"a"}}}`},
		{`meta.k:null`, `{"bool":{"must_not":[{"exists":{"field":"meta.k"}}]}}`},
		{`meta.k:a*`, `{"wildcard":{"meta.k":{"value":"a*","case_insensitive":true}}}`},
		{`title:roam~2`, `{"fuzzy":{"title":{"value":"roam","fuzziness":2}}}`},
		{`title:roam~`, `{"fuzzy":{"title":{"value":"roam","fuzziness":1}}}`},
		{`status:open OR status:closed`, `{"bool":{"should":[{"term":{"status":{"value":"open"}}},{"term":{"status":{"value":"closed"}}}],"minimum_should_match":1}}`},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got, err := p.ParseToElasticsearch(tt.query)
			if err != nil {
				t.Fatalf("ParseToElasticsearch(%q) error = %v", tt.query, err)
			}
			var want any
			if err := json.Unmarshal([]byte(tt.want), &want); err != nil {
				t.Fatalf("bad want JSON: %v", err)
			}
			if g := jsonValue(t, got); !reflect.DeepEqual(g, want) {
				gb, _ := json.Marshal(got)
				t.Errorf("ParseToElasticsearch(%q)\n got: %s\nwant: %s", tt.query, gb, tt.want)
			}
		})
	}
}

// Elasticsearch refuses a query nested deeper than 30 levels. go-lucene builds
// AND and OR as binary trees, so a chain of one operator must render as one
// flat clause list, or 31 ANDed terms fail on the server.
func TestElasticsearchFlattensChains(t *testing.T) {
	type wide struct {
		A string `json:"a"`
		B string `json:"b"`
		C string `json:"c"`
		D string `json:"d"`
	}
	p, err := NewParser(wide{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		query string
		occur string
		count int
	}{
		{`a:1 AND b:2 AND c:3 AND d:4`, "filter", 4},
		{`a:1 OR b:2 OR c:3 OR d:4`, "should", 4},
		{`room`, "should", 4},
		{strings.Repeat(`a:1 AND `, 39) + `a:1`, "filter", 40},
	}
	for _, tt := range tests {
		got, err := p.ParseToElasticsearch(tt.query)
		if err != nil {
			t.Fatalf("ParseToElasticsearch(%.40q) error = %v", tt.query, err)
		}
		clauses, _ := got["bool"].(map[string]any)[tt.occur].([]map[string]any)
		if len(clauses) != tt.count {
			b, _ := json.Marshal(got)
			t.Errorf("ParseToElasticsearch(%.40q) = %.300s, want one bool.%s of %d clauses", tt.query, b, tt.occur, tt.count)
		}
	}

	// An OR inside an AND keeps its own bool: flattening never changes meaning.
	got, err := p.ParseToElasticsearch(`a:1 AND (b:2 OR c:3)`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"bool":{"filter":[{"term":{"a":{"value":"1"}}},{"bool":{"should":[{"term":{"b":{"value":"2"}}},{"term":{"c":{"value":"3"}}}],"minimum_should_match":1}}]}}`
	var w any
	_ = json.Unmarshal([]byte(want), &w)
	if !reflect.DeepEqual(jsonValue(t, got), w) {
		b, _ := json.Marshal(got)
		t.Errorf("a:1 AND (b:2 OR c:3)\n got: %s\nwant: %s", b, want)
	}
}

// A uuid is stored as a keyword, so a wildcard on it is valid. A number, a
// boolean or a date is not, and Elasticsearch would refuse it with a server
// error, so the renderer refuses it first.
func TestElasticsearchWildcardByFieldType(t *testing.T) {
	type typed struct {
		Ref     uuid.UUID `json:"ref"`
		Done    bool      `json:"done"`
		Created time.Time `json:"created"`
	}
	p, err := NewParser(typed{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ParseToElasticsearch(`ref:0a1b*`); err != nil {
		t.Errorf("ref:0a1b* error = %v, want a wildcard", err)
	}
	for _, q := range []string{`done:t*`, `created:2024*`} {
		if _, err := p.ParseToElasticsearch(q); err == nil || !strings.Contains(err.Error(), "wildcard matching is not supported on non-string field") {
			t.Errorf("%s error = %v, want the non-string wildcard error", q, err)
		}
	}
}

func TestElasticsearchErrors(t *testing.T) {
	p := newESParser(t)

	for _, q := range []string{`nope:x`, `nope.k:a`} {
		_, err := p.ParseToElasticsearch(q)
		var invalid *InvalidFieldError
		if !errors.As(err, &invalid) {
			t.Errorf("%s error = %v, want *InvalidFieldError", q, err)
		}
	}

	tests := []struct {
		query   string
		wantErr string
	}{
		{`year:abc`, `invalid value "abc" for integer field 'year'`},
		{`year:[* TO *]`, `both range bounds cannot be wildcards`},
		{`year:[a TO 5]`, `invalid value "a" for integer field 'year'`},
		{`year:>abc`, `invalid value "abc" for integer field 'year'`},
		{`tags:[a TO b]`, `range queries are not supported on array field 'tags'`},
		{`tags:>a`, `operator GREATER is not supported on array field 'tags'`},
		{`nums:abc`, `invalid value "abc" for integer array field 'nums'`},
		{`title:x^2`, `boost operator (^) is not supported in Elasticsearch filtering`},
		{`tags:go~1`, `fuzzy matching is not supported on array field 'tags'`},
		{`year:2020~1`, `fuzzy matching is not supported on non-string field 'year'`},
		{`nums:1*`, `wildcard matching is not supported on non-string array field 'nums'; use containment (nums:value)`},
		{`year:20*`, `wildcard matching is not supported on non-string field 'year'`},
		{`title:/ro.m/`, `regexp queries (field:/pattern/) are not supported`},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			_, err := p.ParseToElasticsearch(tt.query)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ParseToElasticsearch(%q) error = %v, want it to contain %q", tt.query, err, tt.wantErr)
			}
		})
	}
}

// TestElasticsearchUserTextStaysAValue sends query text that tries to become
// Query DSL. Every string the user typed must land as the value of a clause:
// no key may run code or parse text, and a "field" key names a model field.
func TestElasticsearchUserTextStaysAValue(t *testing.T) {
	p := newESParser(t)
	fields := map[string]bool{}
	for _, f := range p.Fields {
		fields[f.Name] = true
	}
	forbidden := map[string]bool{"script": true, "query_string": true, "simple_query_string": true}
	queries := []string{
		`title:"x\"}},{\"script\":{\"source\":\"1\"}}"`,
		`title:query_string*`,
		`title:"a\\b"`,
		`title:*`,
		`script`,
	}
	withPattern := WithElasticsearchFields(map[string]ElasticsearchField{"title": {Pattern: "title.lc"}})
	withText := WithElasticsearchFields(map[string]ElasticsearchField{"title": {Pattern: "title.lc", Text: "title.text"}})
	for _, q := range queries {
		for _, opts := range [][]ElasticsearchOption{nil, {withPattern}, {withText}} {
			t.Run(q, func(t *testing.T) {
				got, err := p.ParseToElasticsearch(q, opts...)
				if err != nil {
					t.Fatalf("ParseToElasticsearch(%q) error = %v", q, err)
				}
				var walk func(key string, v any)
				walk = func(key string, v any) {
					switch n := v.(type) {
					case map[string]any:
						for k, child := range n {
							if forbidden[k] {
								t.Errorf("ParseToElasticsearch(%q) has key %q", q, k)
							}
							walk(k, child)
						}
					case []any:
						for _, child := range n {
							walk(key, child)
						}
					case string:
						switch {
						case key == "value", key == "query":
						case key == "field" && fields[n]:
						case key == "fuzziness" && n == "AUTO", key == "operator" && n == "and":
						default:
							t.Errorf("ParseToElasticsearch(%q): string %q under key %q, want only under \"value\" or \"query\"", q, n, key)
						}
					}
				}
				walk("", jsonValue(t, got))
			})
		}
	}
}

// assertDSL fails when got does not serialise to the same document as want.
func assertDSL(t *testing.T, query string, got map[string]any, want string) {
	t.Helper()
	var w any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want JSON for %q: %v", query, err)
	}
	if !reflect.DeepEqual(jsonValue(t, got), w) {
		b, _ := json.Marshal(got)
		t.Errorf("ParseToElasticsearch(%q)\n got: %s\nwant: %s", query, b, want)
	}
}

// With Pattern, wildcards and fuzzy go to a lowercase-normalized sub-field, so
// case folds in every language. A term stays on the exact field, as on SQL.
func TestElasticsearchFieldOptions(t *testing.T) {
	p := newESParser(t)
	opt := WithElasticsearchFields(map[string]ElasticsearchField{"title": {Pattern: "title.lc"}})
	tests := []struct {
		query string
		want  string
	}{
		{`title:тръб*`, `{"wildcard":{"title.lc":{"value":"тръб*","case_insensitive":true}}}`},
		{`title:тръба~1`, `{"fuzzy":{"title.lc":{"value":"тръба","fuzziness":1}}}`},
		{`title:Тръба`, `{"term":{"title":{"value":"Тръба"}}}`},
		{`title:*`, `{"exists":{"field":"title"}}`},
		{`status:open`, `{"term":{"status":{"value":"open"}}}`},
	}
	for _, tt := range tests {
		got, err := p.ParseToElasticsearch(tt.query, opt)
		if err != nil {
			t.Fatalf("ParseToElasticsearch(%q) error = %v", tt.query, err)
		}
		assertDSL(t, tt.query, got, tt.want)
	}

	for opts, wantErr := range map[string]struct {
		fields map[string]ElasticsearchField
		err    string
	}{
		"unknown": {map[string]ElasticsearchField{"nmae": {Pattern: "nmae.lc"}}, `unknown field 'nmae' in ElasticsearchField options; valid fields are:`},
		"number":  {map[string]ElasticsearchField{"year": {Pattern: "year.lc"}}, `field 'year' is not a string; Pattern and Text apply to string fields only`},
	} {
		_, err := p.ParseToElasticsearch(`title:x`, WithElasticsearchFields(wantErr.fields))
		if err == nil || !strings.Contains(err.Error(), wantErr.err) {
			t.Errorf("%s: error = %v, want it to contain %q", opts, err, wantErr.err)
		}
	}
}

// With Text, a word typed without a field is ranked full text: a match query
// in bool.must. A field without Text keeps its *word* wildcard. A quoted
// phrase and a typed wildcard stay exact, as without the option.
func TestElasticsearchTextFields(t *testing.T) {
	p := newESParser(t)
	opt := WithElasticsearchFields(map[string]ElasticsearchField{"title": {Pattern: "title.lc", Text: "title.text"}})
	word := `{"bool":{"should":[{"wildcard":{"id":{"value":"*тръби*","case_insensitive":true}}},{"match":{"title.text":{"query":"тръби","fuzziness":"AUTO","operator":"and"}}}],"minimum_should_match":1}}`
	tests := []struct {
		query string
		want  string
	}{
		{`тръби`, word},
		{`тръби AND status:open`, `{"bool":{"filter":[{"term":{"status":{"value":"open"}}}],"must":[` + word + `]}}`},
		{`status:open тръби`, `{"bool":{"filter":[{"term":{"status":{"value":"open"}}}],"must":[` + word + `]}}`},
		{`-тръби`, `{"bool":{"must_not":[` + word + `]}}`},
		{`"тръба pvc"`, `{"bool":{"should":[{"term":{"id":{"value":"тръба pvc"}}},{"term":{"title":{"value":"тръба pvc"}}}],"minimum_should_match":1}}`},
		{`title:*тръби*`, `{"wildcard":{"title.lc":{"value":"*тръби*","case_insensitive":true}}}`},
		{`тръб*`, `{"bool":{"should":[{"wildcard":{"id":{"value":"тръб*","case_insensitive":true}}},{"wildcard":{"title.lc":{"value":"тръб*","case_insensitive":true}}}],"minimum_should_match":1}}`},
	}
	for _, tt := range tests {
		got, err := p.ParseToElasticsearch(tt.query, opt)
		if err != nil {
			t.Fatalf("ParseToElasticsearch(%q) error = %v", tt.query, err)
		}
		assertDSL(t, tt.query, got, tt.want)
	}
}

// The text marker is internal: an unknown-field error never offers it.
func TestElasticsearchTextMarkerStaysInternal(t *testing.T) {
	p := newESParser(t)
	opt := WithElasticsearchFields(map[string]ElasticsearchField{"title": {Text: "title.text"}})
	_, err := p.ParseToElasticsearch(`nope:x`, opt)
	var invalid *InvalidFieldError
	if !errors.As(err, &invalid) || strings.Contains(err.Error(), textField) {
		t.Errorf("nope:x error = %v, want *InvalidFieldError without %s", err, textField)
	}
}
