package lucene

import (
	"strings"
	"testing"
)

// Test model definitions
type BasicModel struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type BooleanModel struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Role   string `json:"role"`
}

type RangeModel struct {
	Age  int    `json:"age"`
	Date string `json:"date"`
}

type TextModel struct {
	Description string `json:"description"`
	Title       string `json:"title"`
	Name        string `json:"name"`
}

type ComplexModel struct {
	Name   string `json:"name"`
	Age    int    `json:"age"`
	Status string `json:"status"`
	Email  string `json:"email"`
}

// JSONB types for testing
type JSONBType map[string]interface{}

type JSONBModel struct {
	Metadata JSONBType `json:"metadata"`
}

type MixedModel struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Labels      JSONBType `json:"labels"`
	Metadata    JSONBType `json:"metadata"`
}

type NullModel struct {
	Name          string `json:"name"`
	ParentID      string `json:"parent_id"`
	DeletedAt     string `json:"deleted_at"`
	AttachmentIDs string `json:"attachment_ids"`
}

// TestBasicFieldSearch tests basic field:value queries
func TestBasicFieldSearch(t *testing.T) {
	parser, err := NewParser(BasicModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name     string
		query    string
		wantSQL  string
		wantVals int
	}{
		{
			name:     "simple field query",
			query:    "name:john",
			wantSQL:  `"name" = $1`,
			wantVals: 1,
		},
		{
			name:     "wildcard prefix",
			query:    "name:john*",
			wantSQL:  `"name"::text ILIKE $1`,
			wantVals: 1,
		},
		{
			name:     "wildcard suffix",
			query:    "name:*john",
			wantSQL:  `"name"::text ILIKE $1`,
			wantVals: 1,
		},
		{
			name:     "wildcard contains",
			query:    "name:*john*",
			wantSQL:  `"name"::text ILIKE $1`,
			wantVals: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, vals, err := parser.ParseToSQL(tt.query, "postgresql")
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			if !strings.Contains(sql, tt.wantSQL) {
				t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, tt.wantSQL)
			}
			if len(vals) != tt.wantVals {
				t.Errorf("ParseToSQL() vals count = %v, want %v", len(vals), tt.wantVals)
			}
		})
	}
}

// TestBooleanOperators tests AND, OR, NOT operators
func TestBooleanOperators(t *testing.T) {
	parser, err := NewParser(BooleanModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "AND operator",
			query:   "name:john AND status:active",
			wantSQL: []string{`"name"`, `"status"`, "AND"},
		},
		{
			name:    "OR operator",
			query:   "name:john OR name:jane",
			wantSQL: []string{`"name"`, "OR"},
		},
		{
			name:    "NOT operator",
			query:   "name:john NOT status:inactive",
			wantSQL: []string{`"name"`, `"status"`, "NOT"},
		},
		{
			name:    "complex nested",
			query:   "(name:john OR name:jane) AND status:active",
			wantSQL: []string{`"name"`, `"status"`, "OR", "AND"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
		})
	}
}

// TestRequiredProhibited tests + and - operators
func TestRequiredProhibited(t *testing.T) {
	parser, err := NewParser(BooleanModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "required term",
			query:   "+name:john",
			wantSQL: []string{`"name"`},
		},
		{
			name:    "prohibited term",
			query:   "-status:inactive",
			wantSQL: []string{"NOT", `"status"`},
		},
		{
			name:    "mixed required and prohibited",
			query:   "+name:john -status:inactive",
			wantSQL: []string{`"name"`, "NOT", `"status"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
		})
	}
}

// TestRangeQueries tests range query syntax
func TestRangeQueries(t *testing.T) {
	parser, err := NewParser(RangeModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "inclusive range",
			query:   "age:[25 TO 65]",
			wantSQL: []string{`"age"`, "BETWEEN"},
		},
		{
			name:    "exclusive range",
			query:   "age:{25 TO 65}",
			wantSQL: []string{`"age"`, ">", "<"},
		},
		{
			name:    "open-ended range min",
			query:   "age:[25 TO *]",
			wantSQL: []string{`"age"`, ">="},
		},
		{
			name:    "open-ended range max",
			query:   "age:[* TO 65]",
			wantSQL: []string{`"age"`, "<="},
		},
		{
			name:    "date range",
			query:   "date:[2024-01-01 TO 2024-12-31]",
			wantSQL: []string{`"date"`, "BETWEEN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
		})
	}
}

// TestQuotedPhrases tests quoted phrase handling
func TestQuotedPhrases(t *testing.T) {
	parser, err := NewParser(TextModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "simple quoted phrase",
			query:   `description:"hello world"`,
			wantSQL: []string{`"description"`},
		},
		{
			name:    "phrase with special chars",
			query:   `title:"test-app (v1.0)"`,
			wantSQL: []string{`"title"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
		})
	}
}

// TestEscapedCharacters tests escaped character handling
func TestEscapedCharacters(t *testing.T) {
	parser, err := NewParser(TextModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "escaped colon",
			query:   `name:test\:value`,
			wantSQL: []string{`"name"`},
		},
		{
			name:    "escaped plus",
			query:   `name:C\+\+`,
			wantSQL: []string{`"name"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
		})
	}
}

// TestComplexQueries tests complex query combinations
func TestComplexQueries(t *testing.T) {
	parser, err := NewParser(ComplexModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name      string
		query     string
		wantSQL   []string
		shouldErr bool
	}{
		{
			name:    "complex with ranges and wildcards",
			query:   "(name:john* OR email:test*) AND age:[25 TO 65]",
			wantSQL: []string{`"name"`, `"email"`, `"age"`, "OR", "AND", "BETWEEN"},
		},
		{
			name:    "complex with required and prohibited",
			query:   "+name:john -status:inactive age:[25 TO 65]",
			wantSQL: []string{`"name"`, `"status"`, `"age"`, "NOT"},
		},
		{
			name:    "complex with quoted phrases",
			query:   `name:"John Doe" AND status:active`,
			wantSQL: []string{`"name"`, `"status"`, "AND"},
		},
		{
			name:    "complex nested query",
			query:   "((name:john OR name:jane) AND status:active) OR (status:pending AND age:[18 TO *])",
			wantSQL: []string{`"name"`, `"status"`, `"age"`, "OR", "AND"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.shouldErr {
				t.Fatalf("ParseToSQL() error = %v, shouldErr = %v", err, tt.shouldErr)
			}
			if !tt.shouldErr {
				for _, want := range tt.wantSQL {
					if !strings.Contains(sql, want) {
						t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
					}
				}
			}
		})
	}
}

// TestImplicitSearch tests implicit search across string fields
func TestImplicitSearch(t *testing.T) {
	parser, err := NewParser(TextModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name       string
		query      string
		wantOR     bool
		wantParams int
	}{
		{
			name:       "implicit search",
			query:      "john",
			wantOR:     true,
			wantParams: 3, // name, description, title
		},
		{
			name:       "implicit search with wildcard",
			query:      "john*",
			wantOR:     true,
			wantParams: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := parser.ParseToSQL(tt.query, "postgresql")
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			if tt.wantOR && !strings.Contains(sql, "OR") {
				t.Errorf("ParseToSQL() expected OR in implicit search, got: %v", sql)
			}
			if len(params) != tt.wantParams {
				t.Errorf("ParseToSQL() params count = %v, want %v", len(params), tt.wantParams)
			}
		})
	}
}

// TestJSONBFields tests JSONB field notation
func TestJSONBFields(t *testing.T) {
	parser, err := NewParser(JSONBModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantSQL []string
	}{
		{
			name:    "JSONB field access",
			query:   "metadata.key:value",
			wantSQL: []string{`metadata->>'key'`},
		},
		{
			name:    "JSONB with wildcard",
			query:   "metadata.tags:prod*",
			wantSQL: []string{`metadata->>'tags'`, "ILIKE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if err != nil {
				t.Fatalf("ParseToSQL() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, want)
				}
			}
		})
	}
}

// TestMapOutput tests the legacy map output format
func TestMapOutput(t *testing.T) {
	parser, err := NewParser(BooleanModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	result, err := parser.ParseToMap("name:john AND status:active")
	if err != nil {
		t.Fatalf("ParseToMap() error = %v", err)
	}

	if result == nil {
		t.Errorf("ParseToMap() returned nil")
	}
}

// TestFieldValidation tests field validation for invalid field references
func TestFieldValidation(t *testing.T) {
	parser, err := NewParser(MixedModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name     string
		query    string
		wantErr  bool
		errField string
	}{
		{
			name:    "valid field query",
			query:   "name:john",
			wantErr: false,
		},
		{
			name:    "valid JSONB sub-field",
			query:   "labels.category:prod",
			wantErr: false,
		},
		{
			name:     "invalid field",
			query:    "invalidfield:value",
			wantErr:  true,
			errField: "invalidfield",
		},
		{
			name:     "invalid JSONB base",
			query:    "notjsonb.subfield:value",
			wantErr:  true,
			errField: "notjsonb",
		},
		{
			name:     "sub-field on non-JSONB field",
			query:    "name.subfield:value",
			wantErr:  true,
			errField: "name",
		},
		{
			name:    "implicit search (no explicit fields) - valid",
			query:   "searchterm",
			wantErr: false,
		},
		{
			name:    "mixed valid and implicit",
			query:   "name:john OR searchterm",
			wantErr: false,
		},
		{
			name:     "mixed valid and invalid",
			query:    "name:john OR invalidfield:value",
			wantErr:  true,
			errField: "invalidfield",
		},
		{
			name:    "complex valid query",
			query:   "(name:john OR description:test) AND labels.env:prod",
			wantErr: false,
		},
		{
			name:     "invalid field in complex query",
			query:    "(name:john OR badfield:test) AND status:active",
			wantErr:  true,
			errField: "badfield",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseToSQL() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errField != "" && !strings.Contains(err.Error(), tt.errField) {
				t.Errorf("ParseToSQL() error = %v, want to contain field %v", err, tt.errField)
			}
		})
	}
}

// TestNullValueQueries tests null value handling for IS NULL queries
func TestNullValueQueries(t *testing.T) {
	parser, err := NewParser(NullModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantSQL string
		wantErr bool
	}{
		{
			name:    "field is null (lowercase)",
			query:   "parent_id:null",
			wantSQL: `"parent_id" IS NULL`,
		},
		{
			name:    "field is NULL (uppercase)",
			query:   "parent_id:NULL",
			wantSQL: `"parent_id" IS NULL`,
		},
		{
			name:    "field is Null (mixed case)",
			query:   "parent_id:Null",
			wantSQL: `"parent_id" IS NULL`,
		},
		{
			name:    "parent_id is null",
			query:   "parent_id:null",
			wantSQL: `"parent_id" IS NULL`,
		},
		{
			name:    "combined null with other conditions",
			query:   "name:john AND deleted_at:null",
			wantSQL: `"deleted_at" IS NULL`,
		},
		{
			name:    "NOT null (is not null)",
			query:   "NOT deleted_at:null",
			wantSQL: `NOT(`,
		},
		{
			name:    "nil should be treated as literal value (not NULL)",
			query:   "name:nil",
			wantSQL: `"name" =`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseToSQL() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && !strings.Contains(sql, tt.wantSQL) {
				t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, tt.wantSQL)
			}
		})
	}
}

// TestEmptyAsLiteralValue tests that 'empty' is treated as a literal value
func TestEmptyAsLiteralValue(t *testing.T) {
	parser, err := NewParser(BooleanModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	sql, params, err := parser.ParseToSQL("status:empty", "postgresql")
	if err != nil {
		t.Fatalf("ParseToSQL() error = %v", err)
	}

	if !strings.Contains(sql, `"status" =`) {
		t.Errorf("Expected regular equals query, got: %v", sql)
	}
	if len(params) != 1 || params[0] != "empty" {
		t.Errorf("Expected params to contain 'empty', got: %v", params)
	}
}

// TestFuzzySearch tests fuzzy search operator (~)
func TestFuzzySearch(t *testing.T) {
	parser, err := NewParser(MixedModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantSQL string
		wantErr bool
	}{
		{
			name:    "basic fuzzy search",
			query:   "name:roam~",
			wantSQL: "similarity",
		},
		{
			name:    "fuzzy with distance",
			query:   "name:roam~2",
			wantSQL: "similarity",
		},
		{
			name:    "fuzzy on JSONB field",
			query:   "labels.tag:prod~",
			wantSQL: "similarity",
		},
		{
			name:    "fuzzy combined with other conditions",
			query:   "name:test~ AND status:active",
			wantSQL: "similarity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseToSQL() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && !strings.Contains(sql, tt.wantSQL) {
				t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, tt.wantSQL)
			}
		})
	}
}

// TestEscaping tests that special characters can be escaped
func TestEscaping(t *testing.T) {
	type EscapeModel struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Path    string `json:"path"`
	}

	parser, err := NewParser(EscapeModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantSQL string
		wantErr bool
	}{
		{
			name:    "escaped plus sign",
			query:   `name:C\+\+`,
			wantSQL: `"name"`,
		},
		{
			name:    "escaped colon",
			query:   `name:test\:value`,
			wantSQL: `"name"`,
		},
		{
			name:    "escaped path separator",
			query:   `path:\/usr\/bin`,
			wantSQL: `"path"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseToSQL() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && !strings.Contains(sql, tt.wantSQL) {
				t.Errorf("ParseToSQL() sql = %v, want to contain %v", sql, tt.wantSQL)
			}
		})
	}
}

// TestBoostOperatorError tests that boost operator returns a clear error
func TestBoostOperatorError(t *testing.T) {
	parser, err := NewParser(BooleanModel{})
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{
			name:    "boost operator",
			query:   "name:john^2",
			wantErr: "boost",
		},
		{
			name:    "boost in compound query",
			query:   "name:john^2 AND status:active",
			wantErr: "boost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if err == nil {
				t.Errorf("ParseToSQL(%q) expected error but got none", tt.query)
				return
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
				t.Errorf("ParseToSQL(%q) error = %v, want to contain %v", tt.query, err, tt.wantErr)
			}
		})
	}
}

// BenchmarkParser benchmarks the parser performance
func BenchmarkParser(b *testing.B) {
	parser, _ := NewParser(ComplexModel{})
	query := `(name:john* OR email:*@example.com) AND (status:active OR status:pending) AND age:[25 TO 65]`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = parser.ParseToSQL(query, "postgresql")
	}
}
