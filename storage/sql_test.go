package storage

import (
	"maps"
	"testing"
)

func TestListRejectsMaliciousSortKey(t *testing.T) {
	// Reset singleton so we get a fresh SQLite adapter
	prev := sqlAdapterInstance
	sqlAdapterInstance = nil
	t.Cleanup(func() { sqlAdapterInstance = prev })
	adapter := GetSQLAdapterInstance(map[string]string{
		"provider": "sqlite",
	})
	type Row struct {
		ID string `gorm:"primaryKey"`
	}
	_ = adapter.DB.AutoMigrate(&Row{})

	var rows []Row
	_, err := adapter.List(&rows, "id; DROP TABLE rows", map[string]any{}, 10, "")
	if err == nil {
		t.Error("expected error for malicious sortKey, got nil")
	}
}

func TestValidateSortKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple column", "id", false},
		{"snake_case column", "created_at", false},
		{"mixed case", "createdAt", false},
		{"with numbers", "field1", false},
		{"empty string", "", true},
		{"leading digit", "1field", true},
		{"dot notation injection", "id; DROP TABLE users", true},
		{"semicolon", "id;DROP", true},
		{"space", "col name", true},
		{"table.column dot", "t.col", true},
		{"SQL comment", "id--", true},
		{"single quote", "id'", true},
		{"underscore prefix", "_ts", false},          // CosmosDB system fields like _ts are valid
		{"null byte", "id\x00DROP", true},            // null byte injection rejected
		{"unicode lookalike", "iа", true},            // Cyrillic а (U+0430) rejected, not ASCII
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSortKey(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSortKey(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestExtractSortDirection(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected SortingDirection
		wantErr  bool
	}{
		{"default when missing", map[string]any{}, Ascending, false},
		{"ASC explicit", map[string]any{SortDirectionKey: "ASC"}, Ascending, false},
		{"DESC", map[string]any{SortDirectionKey: "DESC"}, Descending, false},
		{"lowercase desc", map[string]any{SortDirectionKey: "desc"}, Descending, false},
		{"invalid returns error", map[string]any{SortDirectionKey: "SIDEWAYS"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractSortDirection(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractSortDirection(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
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
			if !maps.Equal(got, tt.expected) {
				t.Errorf("extractParams(%v) = %v; want %v", tt.input, got, tt.expected)
			}
		})
	}
}
