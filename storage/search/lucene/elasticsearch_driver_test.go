package lucene

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
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
