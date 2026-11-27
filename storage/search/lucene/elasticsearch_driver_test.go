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

// go-lucene parses field:(a OR b) with each value on its default field ("id"
// for esDoc). Each value must apply to the outer field instead.
func TestElasticsearchGroupUsesOuterField(t *testing.T) {
	p := newESParser(t)
	for _, q := range []string{`status:(open OR null)`, `status:(open AND closed)`, `tags:(go OR rust)`} {
		got, err := p.ParseToElasticsearch(q)
		if err != nil {
			t.Fatalf("ParseToElasticsearch(%q) error = %v", q, err)
		}
		b, _ := json.Marshal(got)
		if strings.Contains(string(b), `"id"`) {
			t.Errorf("ParseToElasticsearch(%q) = %s, names the default field", q, b)
		}
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

	_, err := p.ParseToElasticsearch(`nope:x`)
	var invalid *InvalidFieldError
	if !errors.As(err, &invalid) {
		t.Errorf("nope:x error = %v, want *InvalidFieldError", err)
	}

	tests := []struct {
		query   string
		wantErr string
	}{
		{`year:abc`, `invalid value "abc" for integer field 'year'`},
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
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			got, err := p.ParseToElasticsearch(q)
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
					case key == "value":
					case key == "field" && fields[n]:
					default:
						t.Errorf("ParseToElasticsearch(%q): string %q under key %q, want only under \"value\"", q, n, key)
					}
				}
			}
			walk("", jsonValue(t, got))
		})
	}
}
