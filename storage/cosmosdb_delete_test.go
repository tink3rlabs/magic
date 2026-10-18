package storage

import (
	"context"
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
