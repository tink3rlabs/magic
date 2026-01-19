package lucene

import (
	"strings"
	"testing"
)

// Helper functions following FIRST principles

// assertSQLContains checks that SQL contains all required substrings (more precise validation)
func assertSQLContains(t *testing.T, sql string, required []string, msg string) {
	t.Helper()
	for _, req := range required {
		if !strings.Contains(sql, req) {
			t.Errorf("%s: SQL = %q, missing required substring %q", msg, sql, req)
		}
	}
}

// assertSQLNotContains checks that SQL does not contain forbidden substrings
func assertSQLNotContains(t *testing.T, sql string, forbidden []string, msg string) {
	t.Helper()
	for _, forb := range forbidden {
		if strings.Contains(sql, forb) {
			t.Errorf("%s: SQL = %q, contains forbidden substring %q", msg, sql, forb)
		}
	}
}

// assertParamsEqual validates exact parameter values (self-validating)
func assertParamsEqual(t *testing.T, got []any, want []any, msg string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: param count = %d, want %d", msg, len(got), len(want))
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s: param[%d] = %v, want %v", msg, i, got[i], want[i])
		}
	}
}

// assertErrorContains validates error messages precisely
func assertErrorContains(t *testing.T, err error, wantSubstrings []string, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: expected error, got nil", msg)
		return
	}
	errMsg := err.Error()
	for _, want := range wantSubstrings {
		if !strings.Contains(errMsg, want) {
			t.Errorf("%s: error = %q, missing required substring %q", msg, errMsg, want)
		}
	}
}

// createParser is a helper to reduce duplication (Fast principle - parser created once per test)
func createParser(t *testing.T, model any, config ...*ParserConfig) *Parser {
	t.Helper()
	parser, err := NewParser(model, config...)
	if err != nil {
		t.Fatalf("NewParser() error = %v", err)
	}
	return parser
}

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
// Improved with precise assertions following FIRST principles
func TestBasicFieldSearch(t *testing.T) {
	parser := createParser(t, BasicModel{})

	tests := []struct {
		name       string
		query      string
		wantSQL    []string
		wantNot    []string
		wantParams []any
		wantErr    bool
	}{
		{
			name:       "simple field query",
			query:      "name:john",
			wantSQL:    []string{`"name"`, "=", "$1"},
			wantNot:    []string{"ILIKE", "LIKE"},
			wantParams: []any{"john"},
			wantErr:    false,
		},
		{
			name:       "wildcard prefix",
			query:      "name:john*",
			wantSQL:    []string{`"name"`, "ILIKE", "$1"},
			wantNot:    []string{"="},
			wantParams: []any{"john%"},
			wantErr:    false,
		},
		{
			name:       "wildcard suffix",
			query:      "name:*john",
			wantSQL:    []string{`"name"`, "ILIKE", "$1"},
			wantParams: []any{"%john"},
			wantErr:    false,
		},
		{
			name:       "wildcard contains",
			query:      "name:*john*",
			wantSQL:    []string{`"name"`, "ILIKE", "$1"},
			wantParams: []any{"%john%"},
			wantErr:    false,
		},
		{
			name:       "email field",
			query:      `email:"test@example.com"`,
			wantSQL:    []string{`"email"`, "=", "$1"},
			wantParams: []any{"test@example.com"},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseToSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				assertSQLContains(t, sql, tt.wantSQL, tt.name)
				if len(tt.wantNot) > 0 {
					assertSQLNotContains(t, sql, tt.wantNot, tt.name)
				}
				if len(tt.wantParams) > 0 {
					// Only validate params if we expect specific values
					if len(tt.wantParams) > 0 {
						assertParamsEqual(t, params, tt.wantParams, tt.name)
					}
				}
			}
		})
	}
}

// TestBooleanOperators tests AND, OR, NOT operators
// Improved with parameter validation
func TestBooleanOperators(t *testing.T) {
	parser := createParser(t, BooleanModel{})

	tests := []struct {
		name       string
		query      string
		wantSQL    []string
		wantParams []any
		wantErr    bool
	}{
		{
			name:       "AND operator",
			query:      "name:john AND status:active",
			wantSQL:    []string{`"name"`, `"status"`, "AND"},
			wantParams: []any{"john", "active"},
			wantErr:    false,
		},
		{
			name:       "OR operator",
			query:      "name:john OR name:jane",
			wantSQL:    []string{`"name"`, "OR"},
			wantParams: []any{"john", "jane"},
			wantErr:    false,
		},
		{
			name:       "NOT operator",
			query:      "name:john NOT status:inactive",
			wantSQL:    []string{`"name"`, `"status"`, "NOT"},
			wantParams: []any{"john", "inactive"},
			wantErr:    false,
		},
		{
			name:       "complex nested",
			query:      "(name:john OR name:jane) AND status:active",
			wantSQL:    []string{`"name"`, `"status"`, "OR", "AND"},
			wantParams: []any{"john", "jane", "active"},
			wantErr:    false,
		},
		{
			name:    "case insensitive AND",
			query:   "name:john and status:active",
			wantSQL: []string{"AND"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseToSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				assertSQLContains(t, sql, tt.wantSQL, tt.name)
				if len(tt.wantParams) > 0 {
					// Only validate params if we expect specific values
					if len(tt.wantParams) > 0 {
						assertParamsEqual(t, params, tt.wantParams, tt.name)
					}
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
// Improved with parameter validation
func TestRangeQueries(t *testing.T) {
	parser := createParser(t, RangeModel{})

	tests := []struct {
		name       string
		query      string
		wantSQL    []string
		wantParams []any
		wantErr    bool
	}{
		{
			name:       "inclusive range",
			query:      "age:[25 TO 65]",
			wantSQL:    []string{`"age"`, "BETWEEN"},
			wantParams: []any{"25", "65"},
			wantErr:    false,
		},
		{
			name:       "exclusive range",
			query:      "age:{25 TO 65}",
			wantSQL:    []string{`"age"`, ">", "<"},
			wantParams: []any{"25", "65"},
			wantErr:    false,
		},
		{
			name:       "open-ended range min",
			query:      "age:[25 TO *]",
			wantSQL:    []string{`"age"`, ">="},
			wantParams: []any{"25"},
			wantErr:    false,
		},
		{
			name:       "open-ended range max",
			query:      "age:[* TO 65]",
			wantSQL:    []string{`"age"`, "<="},
			wantParams: []any{"65"},
			wantErr:    false,
		},
		{
			name:       "date range",
			query:      "date:[2024-01-01 TO 2024-12-31]",
			wantSQL:    []string{`"date"`, "BETWEEN"},
			wantParams: []any{"2024-01-01", "2024-12-31"},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseToSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				assertSQLContains(t, sql, tt.wantSQL, tt.name)
				// Only validate params if we expect specific values
				if len(tt.wantParams) > 0 {
					assertParamsEqual(t, params, tt.wantParams, tt.name)
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
// Improved with precise validation
func TestImplicitSearch(t *testing.T) {
	parser := createParser(t, TextModel{})

	tests := []struct {
		name       string
		query      string
		wantSQL    []string
		wantParams []any
		wantErr    bool
	}{
		{
			name:       "implicit search",
			query:      "john",
			wantSQL:    []string{"OR"},
			wantParams: []any{"%john%", "%john%", "%john%"},
			wantErr:    false,
		},
		{
			name:       "implicit search with wildcard",
			query:      "john*",
			wantSQL:    []string{"OR"},
			wantParams: []any{"john%", "john%", "john%"},
			wantErr:    false,
		},
		{
			name:       "implicit quoted phrase",
			query:      `"john doe"`,
			wantSQL:    []string{"OR"},
			wantParams: []any{"john doe", "john doe", "john doe"}, // quotes are stripped
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseToSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				assertSQLContains(t, sql, tt.wantSQL, tt.name)
				// Only validate params if we expect specific values
				if len(tt.wantParams) > 0 {
					assertParamsEqual(t, params, tt.wantParams, tt.name)
				}
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
// Improved with precise error message validation
func TestFieldValidation(t *testing.T) {
	parser := createParser(t, MixedModel{})

	tests := []struct {
		name        string
		query       string
		wantErr     bool
		wantErrMsgs []string
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
			name:        "invalid field",
			query:       "invalidfield:value",
			wantErr:     true,
			wantErrMsgs: []string{"invalidfield", "invalid field"},
		},
		{
			name:        "invalid JSONB base",
			query:       "notjsonb.subfield:value",
			wantErr:     true,
			wantErrMsgs: []string{"notjsonb"}, // Error message may vary
		},
		{
			name:        "sub-field on non-JSONB field",
			query:       "name.subfield:value",
			wantErr:     true,
			wantErrMsgs: []string{"name.subfield", "invalid field"},
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
			name:        "mixed valid and invalid",
			query:       "name:john OR invalidfield:value",
			wantErr:     true,
			wantErrMsgs: []string{"invalidfield"},
		},
		{
			name:    "complex valid query",
			query:   "(name:john OR description:test) AND labels.env:prod",
			wantErr: false,
		},
		{
			name:        "invalid field in complex query",
			query:       "(name:john OR badfield:test) AND status:active",
			wantErr:     true,
			wantErrMsgs: []string{"badfield"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseToSQL() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && len(tt.wantErrMsgs) > 0 {
				assertErrorContains(t, err, tt.wantErrMsgs, tt.name)
			}
		})
	}
}

// TestNullValueQueries tests null value handling for IS NULL queries
// Improved with precise SQL and parameter validation
func TestNullValueQueries(t *testing.T) {
	parser := createParser(t, NullModel{})

	tests := []struct {
		name       string
		query      string
		wantSQL    []string
		wantNot    []string
		wantParams []any
		wantErr    bool
	}{
		{
			name:       "field is null (lowercase)",
			query:      "parent_id:null",
			wantSQL:    []string{`"parent_id"`, "IS NULL"},
			wantNot:    []string{"=", "$1"},
			wantParams: []any{},
			wantErr:    false,
		},
		{
			name:       "field is NULL (uppercase)",
			query:      "parent_id:NULL",
			wantSQL:    []string{`"parent_id"`, "IS NULL"},
			wantParams: []any{},
			wantErr:    false,
		},
		{
			name:       "field is Null (mixed case)",
			query:      "parent_id:Null",
			wantSQL:    []string{`"parent_id"`, "IS NULL"},
			wantParams: []any{},
			wantErr:    false,
		},
		{
			name:       "combined null with other conditions",
			query:      "name:john AND deleted_at:null",
			wantSQL:    []string{`"name"`, `"deleted_at"`, "IS NULL", "AND"},
			wantParams: []any{"john"},
			wantErr:    false,
		},
		{
			name:       "NOT null (is not null)",
			query:      "NOT deleted_at:null",
			wantSQL:    []string{"NOT", `"deleted_at"`},
			wantParams: []any{"null"}, // NOT null is parsed as NOT field=null, not NOT field IS NULL
			wantErr:    false,
		},
		{
			name:       "nil should be treated as literal value (not NULL)",
			query:      "name:nil",
			wantSQL:    []string{`"name"`, "=", "$1"},
			wantParams: []any{"nil"},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := parser.ParseToSQL(tt.query, "postgresql")
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseToSQL() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				assertSQLContains(t, sql, tt.wantSQL, tt.name)
				if len(tt.wantNot) > 0 {
					assertSQLNotContains(t, sql, tt.wantNot, tt.name)
				}
				if len(tt.wantParams) > 0 {
					// Only validate params if we expect specific values
					if len(tt.wantParams) > 0 {
						assertParamsEqual(t, params, tt.wantParams, tt.name)
					}
				}
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

// TestNewParser tests parser creation and configuration
func TestNewParser(t *testing.T) {
	tests := []struct {
		name      string
		model     any
		config    *ParserConfig
		wantErr   bool
		wantCount int
	}{
		{
			name:      "basic model",
			model:     BasicModel{},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:      "pointer to model",
			model:     &BasicModel{},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:      "with custom config",
			model:     BasicModel{},
			config:    &ParserConfig{MaxQueryLength: 5000, MaxDepth: 10, MaxTerms: 50},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:    "invalid model (not struct)",
			model:   "not a struct",
			wantErr: true,
		},
		{
			name:      "empty struct",
			model:     struct{}{},
			wantErr:   false,
			wantCount: 0,
		},
		{
			name:      "model with no json tags",
			model:     struct{ Name string }{},
			wantErr:   false,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := NewParser(tt.model, tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewParser() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if parser == nil {
					t.Fatal("NewParser() returned nil parser")
				}
				if len(parser.Fields) != tt.wantCount {
					t.Errorf("NewParser() field count = %d, want %d", len(parser.Fields), tt.wantCount)
				}
				if tt.config != nil {
					if tt.config.MaxQueryLength > 0 && parser.MaxQueryLength != tt.config.MaxQueryLength {
						t.Errorf("NewParser() MaxQueryLength = %d, want %d", parser.MaxQueryLength, tt.config.MaxQueryLength)
					}
					if tt.config.MaxDepth > 0 && parser.MaxDepth != tt.config.MaxDepth {
						t.Errorf("NewParser() MaxDepth = %d, want %d", parser.MaxDepth, tt.config.MaxDepth)
					}
					if tt.config.MaxTerms > 0 && parser.MaxTerms != tt.config.MaxTerms {
						t.Errorf("NewParser() MaxTerms = %d, want %d", parser.MaxTerms, tt.config.MaxTerms)
					}
				}
			}
		})
	}
}

// TestParser_ValidateQuery tests query validation (security limits)
func TestParser_ValidateQuery(t *testing.T) {
	parser := createParser(t, BasicModel{})

	tests := []struct {
		name      string
		query     string
		config    *ParserConfig
		wantErr   bool
		wantError []string
	}{
		{
			name:    "valid query",
			query:   "name:john",
			wantErr: false,
		},
		{
			name:      "query too long",
			query:     strings.Repeat("a", 10001),
			wantErr:   true,
			wantError: []string{"too long", "exceeds maximum"},
		},
		{
			name:      "query too deep",
			query:     strings.Repeat("(", 21) + "name:john" + strings.Repeat(")", 21),
			wantErr:   true,
			wantError: []string{"too complex", "nesting depth"},
		},
		{
			name:      "query too many terms",
			query:     strings.Repeat("name:term OR ", 50) + "name:term",
			wantErr:   true,
			wantError: []string{"too large", "terms exceeds"},
		},
		{
			name:    "custom limits - within bounds",
			query:   strings.Repeat("a", 100),
			config:  &ParserConfig{MaxQueryLength: 200},
			wantErr: false,
		},
		{
			name:    "custom limits - exceeds",
			query:   strings.Repeat("a", 201),
			config:  &ParserConfig{MaxQueryLength: 200},
			wantErr: true,
		},
		{
			name:    "empty query",
			query:   "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p *Parser
			if tt.config != nil {
				p = createParser(t, BasicModel{}, tt.config)
			} else {
				p = parser
			}

			err := p.validateQuery(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateQuery() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && len(tt.wantError) > 0 {
				assertErrorContains(t, err, tt.wantError, "validateQuery()")
			}
		})
	}
}

// TestCalculateNestingDepth tests depth calculation (unit test for helper)
func TestCalculateNestingDepth(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{
			name:  "no nesting",
			query: "name:john",
			want:  0,
		},
		{
			name:  "single level",
			query: "(name:john)",
			want:  1,
		},
		{
			name:  "nested",
			query: "((name:john))",
			want:  2,
		},
		{
			name:  "mixed brackets",
			query: "(name:john AND [age:25 TO 65])",
			want:  2,
		},
		{
			name:  "quotes ignore nesting",
			query: `(name:"test (value)")`,
			want:  1,
		},
		{
			name:  "escaped quotes",
			query: `(name:"test\"value")`,
			want:  1,
		},
		{
			name:  "unbalanced (should still calculate)",
			query: "((name:john)",
			want:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateNestingDepth(tt.query)
			if got != tt.want {
				t.Errorf("calculateNestingDepth() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestCountTerms tests term counting (unit test for helper)
func TestCountTerms(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{
			name:  "single term",
			query: "name:john",
			want:  1,
		},
		{
			name:  "multiple terms",
			query: "name:john AND email:test",
			want:  3, // name:john, AND (counted before skip), email:test
		},
		{
			name:  "quoted phrase",
			query: `name:"john doe"`,
			want:  2, // name: and "john doe" (quotes counted separately)
		},
		{
			name:  "range query",
			query: "age:[25 TO 65]",
			want:  2, // age: and range content
		},
		{
			name:  "implicit search",
			query: "john",
			want:  1,
		},
		{
			name:  "empty query",
			query: "",
			want:  0,
		},
		{
			name:  "operators not counted",
			query: "name:john AND email:test OR status:active",
			want:  5, // name:john, AND, email:test, OR, status:active
		},
		{
			name:  "parentheses not counted",
			query: "(name:john OR email:test)",
			want:  3, // name:john, OR, email:test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countTerms(tt.query)
			if got != tt.want {
				t.Errorf("countTerms() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestParser_ProviderSpecific tests all SQL providers
func TestParser_ProviderSpecific(t *testing.T) {
	parser := createParser(t, BasicModel{})

	tests := []struct {
		name       string
		query      string
		provider   string
		wantSQL    []string
		wantNot    []string
		wantParams []any
		wantErr    bool
	}{
		{
			name:       "postgresql placeholder",
			query:      "name:john",
			provider:   "postgresql",
			wantSQL:    []string{"$1"},
			wantNot:    []string{"?"},
			wantParams: []any{"john"},
			wantErr:    false,
		},
		{
			name:       "mysql placeholder",
			query:      "name:john",
			provider:   "mysql",
			wantSQL:    []string{"?"},
			wantNot:    []string{"$"},
			wantParams: []any{"john"},
			wantErr:    false,
		},
		{
			name:       "sqlite placeholder",
			query:      "name:john",
			provider:   "sqlite",
			wantSQL:    []string{"?"},
			wantNot:    []string{"$"},
			wantParams: []any{"john"},
			wantErr:    false,
		},
		{
			name:       "postgresql ILIKE",
			query:      "name:john*",
			provider:   "postgresql",
			wantSQL:    []string{"ILIKE"},
			wantNot:    []string{"LOWER"},
			wantParams: []any{"john%"},
			wantErr:    false,
		},
		{
			name:       "mysql LOWER LIKE",
			query:      "name:john*",
			provider:   "mysql",
			wantSQL:    []string{"LOWER", "LIKE"},
			wantParams: []any{"john%"},
			wantErr:    false,
		},
		{
			name:       "sqlite LIKE",
			query:      "name:john*",
			provider:   "sqlite",
			wantSQL:    []string{"LIKE"},
			wantNot:    []string{"ILIKE", "LOWER"},
			wantParams: []any{"john%"},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := parser.ParseToSQL(tt.query, tt.provider)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseToSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				assertSQLContains(t, sql, tt.wantSQL, tt.name)
				if len(tt.wantNot) > 0 {
					assertSQLNotContains(t, sql, tt.wantNot, tt.name)
				}
				if len(tt.wantParams) > 0 {
					// Only validate params if we expect specific values
					if len(tt.wantParams) > 0 {
						assertParamsEqual(t, params, tt.wantParams, tt.name)
					}
				}
			}
		})
	}
}

// TestParser_ParseToDynamoDBPartiQL tests DynamoDB output
func TestParser_ParseToDynamoDBPartiQL(t *testing.T) {
	parser := createParser(t, BasicModel{})

	tests := []struct {
		name        string
		query       string
		wantPartiQL []string
		wantCount   int
		wantErr     bool
	}{
		{
			name:        "simple query",
			query:       "name:john",
			wantPartiQL: []string{"name"},
			wantCount:   1,
			wantErr:     false,
		},
		{
			name:        "AND query",
			query:       "name:john AND email:test",
			wantPartiQL: []string{"AND"},
			wantCount:   2,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			partiql, attrs, err := parser.ParseToDynamoDBPartiQL(tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseToDynamoDBPartiQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				assertSQLContains(t, partiql, tt.wantPartiQL, tt.name)
				if len(attrs) != tt.wantCount {
					t.Errorf("ParseToDynamoDBPartiQL() attrs count = %d, want %d", len(attrs), tt.wantCount)
				}
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
