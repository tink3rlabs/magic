package storage

import "testing"

func TestExtractSortDirection(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected SortingDirection
	}{
		{"default when missing", map[string]any{}, Ascending},
		{"ASC explicit", map[string]any{SortDirectionKey: "ASC"}, Ascending},
		{"DESC", map[string]any{SortDirectionKey: "DESC"}, Descending},
		{"lowercase desc", map[string]any{SortDirectionKey: "desc"}, Descending},
		{"invalid falls back to ASC", map[string]any{SortDirectionKey: "SIDEWAYS"}, Ascending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSortDirection(tt.input)
			if got != tt.expected {
				t.Errorf("extractSortDirection(%v) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractParams(t *testing.T) {
	tests := []struct {
		name     string
		input    []map[string]any
		expected map[string]any
	}{
		{"empty input", []map[string]any{}, map[string]any{}},
		{"single map", []map[string]any{{"a": 1}}, map[string]any{"a": 1}},
		{"two maps merged", []map[string]any{{"a": 1}, {"b": 2}}, map[string]any{"a": 1, "b": 2}},
		{"later map wins on collision", []map[string]any{{"a": 1}, {"a": 2}}, map[string]any{"a": 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractParams(tt.input...)
			if len(got) != len(tt.expected) {
				t.Errorf("extractParams len = %d; want %d", len(got), len(tt.expected))
				return
			}
			for k, want := range tt.expected {
				if got[k] != want {
					t.Errorf("extractParams[%q] = %v; want %v", k, got[k], want)
				}
			}
		})
	}
}
