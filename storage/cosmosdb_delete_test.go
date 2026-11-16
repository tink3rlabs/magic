package storage

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCosmosDBDeleteContextRejectsMissingOrNonStringID(t *testing.T) {
	s := &CosmosDBAdapter{}
	cases := []struct {
		name   string
		filter map[string]any
		substr string
	}{
		{"empty filter", map[string]any{}, "id filter"},
		{"no id key", map[string]any{"deleted_at": nil}, "id filter"},
		{"non-string id", map[string]any{"id": 42}, "non-empty string"},
		{"empty string id", map[string]any{"id": ""}, "non-empty string"},
		{"non-string pk", map[string]any{"id": "abc", "pk": 7}, "pk must be"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.DeleteContext(context.Background(), cosmosSampleItem{}, tc.filter)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.substr)
			}
		})
	}
}

func TestCosmosDBDeleteContextAllowsEmptyPartitionKey(t *testing.T) {
	var gotPK string
	s := newFakeCosmosAdapter(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Path == "/" {
			return fakeCosmosJSON(req, `{"id":"fake","writableLocations":[],"readableLocations":[]}`), nil
		}
		if req.Method == http.MethodDelete {
			gotPK = req.Header.Get("x-ms-documentdb-partitionkey")
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}
		t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		return nil, nil
	})

	err := s.DeleteContext(context.Background(), cosmosSampleItem{}, map[string]any{"id": "abc", "pk": ""})
	if err != nil {
		t.Fatalf("empty partition key: %v", err)
	}
	if gotPK != `[""]` {
		t.Fatalf("partition key header = %q; want %q", gotPK, `[""]`)
	}
}
