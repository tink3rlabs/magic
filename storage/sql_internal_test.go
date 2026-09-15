package storage

import (
	"strings"
	"testing"
)

func TestBuildQueryNilOnlyFilterHasNoBindings(t *testing.T) {
	s := &SQLAdapter{}
	query, bindings := s.buildQuery(map[string]any{"deleted_at": nil})
	if query != "deleted_at IS NULL" {
		t.Fatalf("query = %q", query)
	}
	if len(bindings) != 0 {
		t.Fatalf("expected empty bindings, got %#v", bindings)
	}

	query, bindings = s.buildQuery(map[string]any{"id": "1", "deleted_at": nil})
	if !strings.Contains(query, "id = @id") || !strings.Contains(query, "deleted_at IS NULL") {
		t.Fatalf("query = %q", query)
	}
	if bindings["id"] != "1" {
		t.Fatalf("bindings = %#v", bindings)
	}
}
