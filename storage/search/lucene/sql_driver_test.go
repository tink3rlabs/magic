package lucene

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

func TestNewSQLDriver(t *testing.T) {
	tests := []struct {
		name     string
		fields   []FieldInfo
		provider string
		wantErr  bool
	}{
		{
			name:     "postgresql with fields",
			fields:   []FieldInfo{{Name: "name", Type: reflect.TypeOf("")}},
			provider: "postgresql",
			wantErr:  false,
		},
		{
			name:     "mysql with fields",
			fields:   []FieldInfo{{Name: "name", Type: reflect.TypeOf("")}},
			provider: "mysql",
			wantErr:  false,
		},
		{
			name:     "sqlite with fields",
			fields:   []FieldInfo{{Name: "name", Type: reflect.TypeOf("")}},
			provider: "sqlite",
			wantErr:  false,
		},
		{
			name:     "empty fields",
			fields:   []FieldInfo{},
			provider: "postgresql",
			wantErr:  false,
		},
		{
			name: "multiple fields",
			fields: []FieldInfo{
				{Name: "name", Type: reflect.TypeOf("")},
				{Name: "email", Type: reflect.TypeOf("")},
				{Name: "age", Type: reflect.TypeOf(0)},
			},
			provider: "postgresql",
			wantErr:  false,
		},
		{
			name: "duplicate field names returns error (postgresql)",
			fields: []FieldInfo{
				{Name: "name", Type: reflect.TypeOf("")},
				{Name: "name", Type: reflect.TypeOf(0)},
			},
			provider: "postgresql",
			wantErr:  true,
		},
		{
			name: "duplicate field names returns error (mysql)",
			fields: []FieldInfo{
				{Name: "name", Type: reflect.TypeOf("")},
				{Name: "name", Type: reflect.TypeOf(0)},
			},
			provider: "mysql",
			wantErr:  true,
		},
		{
			name: "duplicate field names returns error (sqlite)",
			fields: []FieldInfo{
				{Name: "name", Type: reflect.TypeOf("")},
				{Name: "name", Type: reflect.TypeOf(0)},
			},
			provider: "sqlite",
			wantErr:  true,
		},
		{
			name: "multiple duplicate field names (postgresql)",
			fields: []FieldInfo{
				{Name: "name", Type: reflect.TypeOf("")},
				{Name: "email", Type: reflect.TypeOf("")},
				{Name: "name", Type: reflect.TypeOf(0)},
			},
			provider: "postgresql",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := NewSQLDriver(tt.fields, tt.provider)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSQLDriver() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewSQLDriver() expected error but got nil")
				}
				if driver != nil {
					t.Errorf("NewSQLDriver() expected nil driver on error, got %v", driver)
				}
				if err != nil && !strings.Contains(err.Error(), "duplicate field name") {
					t.Errorf("NewSQLDriver() error message should contain 'duplicate field name', got: %v", err)
				}
				return
			}
			if driver == nil {
				t.Fatalf("NewSQLDriver() returned nil")
			}
			if driver.provider != tt.provider {
				t.Errorf("NewSQLDriver() provider = %v, want %v", driver.provider, tt.provider)
			}
			if len(driver.fields) != len(tt.fields) {
				t.Errorf("NewSQLDriver() fields count = %v, want %v", len(driver.fields), len(tt.fields))
			}
		})
	}
}

func TestSQLDriver_RenderParam(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
		{Name: "email", Type: reflect.TypeOf("")},
	}
	providers := []string{"postgresql", "mysql", "sqlite"}

	tests := []struct {
		name      string
		expr      *expr.Expression
		wantSQL   []string
		wantCount int
		wantErr   bool
	}{
		{
			name: "equals expression",
			expr: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			wantSQL:   []string{`"name"`, "="},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "AND expression",
			expr: &expr.Expression{
				Op: expr.And,
				Left: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("name"),
					Right: &expr.Expression{Op: expr.Literal, Left: "john"},
				},
				Right: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("email"),
					Right: &expr.Expression{Op: expr.Literal, Left: "test@example.com"},
				},
			},
			wantSQL:   []string{"AND"},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:    "nil expression",
			expr:    nil,
			wantErr: false,
		},
	}

	for _, provider := range providers {
		for _, tt := range tests {
			t.Run(provider+"/"+tt.name, func(t *testing.T) {
				driver, err := NewSQLDriver(fields, provider)
				if err != nil {
					t.Fatalf("NewSQLDriver() error = %v", err)
				}
				sql, params, err := driver.RenderParam(tt.expr)
				if (err != nil) != tt.wantErr {
					t.Errorf("RenderParam() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if tt.wantErr {
					return
				}
				if tt.expr == nil {
					if sql != "" {
						t.Errorf("RenderParam() sql = %v, want empty string", sql)
					}
					if len(params) != 0 {
						t.Errorf("RenderParam() params count = %v, want 0", len(params))
					}
					return
				}
				for _, want := range tt.wantSQL {
					if !strings.Contains(sql, want) {
						t.Errorf("RenderParam() sql = %v, want to contain %v", sql, want)
					}
				}
				if len(params) != tt.wantCount {
					t.Errorf("RenderParam() params count = %v, want %v", len(params), tt.wantCount)
				}
				// All providers use ? placeholders; GORM handles $N conversion for PostgreSQL
				if tt.wantCount > 0 && !strings.Contains(sql, "?") {
					t.Errorf("RenderParam() expected ? placeholders for %v, got %v", provider, sql)
				}
			})
		}
	}
}

func TestSQLDriver_RenderLikeOrWild(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
		{Name: "metadata", Type: reflect.TypeOf(map[string]interface{}{})},
	}

	tests := []struct {
		name      string
		provider  string
		expr      *expr.Expression
		wantSQL   []string
		wantCount int
		wantErr   bool
	}{
		{
			name:     "postgresql LIKE regular field",
			provider: "postgresql",
			expr: &expr.Expression{
				Op:    expr.Like,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			wantSQL:   []string{"ILIKE"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "postgresql LIKE JSON field",
			provider: "postgresql",
			expr: &expr.Expression{
				Op:    expr.Like,
				Left:  expr.Column("metadata->>'key'"),
				Right: &expr.Expression{Op: expr.Literal, Left: "value"},
			},
			wantSQL:   []string{"ILIKE"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "mysql LIKE",
			provider: "mysql",
			expr: &expr.Expression{
				Op:    expr.Like,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			wantSQL:   []string{"LOWER", "LIKE"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "sqlite LIKE",
			provider: "sqlite",
			expr: &expr.Expression{
				Op:    expr.Like,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			wantSQL:   []string{"LIKE"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "postgresql WILD",
			provider: "postgresql",
			expr: &expr.Expression{
				Op:    expr.Wild,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john*"},
			},
			wantSQL:   []string{"ILIKE"},
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := NewSQLDriver(fields, tt.provider)
			if err != nil {
				t.Fatalf("NewSQLDriver() error = %v", err)
			}
			sql, params, err := driver.renderLikeOrWild(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("renderLikeOrWild() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, want := range tt.wantSQL {
					if !strings.Contains(sql, want) {
						t.Errorf("renderLikeOrWild() sql = %v, want to contain %v", sql, want)
					}
				}
				if len(params) != tt.wantCount {
					t.Errorf("renderLikeOrWild() params count = %v, want %v", len(params), tt.wantCount)
				}
			}
		})
	}
}

func TestSQLDriver_RenderFuzzy(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
		{Name: "metadata", Type: reflect.TypeOf(map[string]interface{}{})},
	}

	tests := []struct {
		name      string
		provider  string
		expr      *expr.Expression
		wantSQL   []string
		wantCount int
		wantErr   bool
	}{
		{
			name:     "postgresql fuzzy",
			provider: "postgresql",
			expr: &expr.Expression{
				Op: expr.Fuzzy,
				Left: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("name"),
					Right: &expr.Expression{Op: expr.Literal, Left: "roam"},
				},
			},
			wantSQL:   []string{"similarity"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "postgresql fuzzy JSON field",
			provider: "postgresql",
			expr: &expr.Expression{
				Op: expr.Fuzzy,
				Left: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("metadata->>'key'"),
					Right: &expr.Expression{Op: expr.Literal, Left: "value"},
				},
			},
			wantSQL:   []string{"similarity"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "mysql fuzzy",
			provider: "mysql",
			expr: &expr.Expression{
				Op: expr.Fuzzy,
				Left: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("name"),
					Right: &expr.Expression{Op: expr.Literal, Left: "roam"},
				},
			},
			wantSQL:   []string{"SOUNDEX"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "sqlite fuzzy (unsupported)",
			provider: "sqlite",
			expr: &expr.Expression{
				Op: expr.Fuzzy,
				Left: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("name"),
					Right: &expr.Expression{Op: expr.Literal, Left: "roam"},
				},
			},
			wantErr: true,
		},
		{
			name:     "invalid fuzzy expression (not Equals)",
			provider: "postgresql",
			expr: &expr.Expression{
				Op:   expr.Fuzzy,
				Left: &expr.Expression{Op: expr.And},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := NewSQLDriver(fields, tt.provider)
			if err != nil {
				t.Fatalf("NewSQLDriver() error = %v", err)
			}
			sql, params, err := driver.renderFuzzy(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("renderFuzzy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, want := range tt.wantSQL {
					if !strings.Contains(sql, want) {
						t.Errorf("renderFuzzy() sql = %v, want to contain %v", sql, want)
					}
				}
				if len(params) != tt.wantCount {
					t.Errorf("renderFuzzy() params count = %v, want %v", len(params), tt.wantCount)
				}
			}
		})
	}
}

func TestSQLDriver_RenderComparison(t *testing.T) {
	fields := []FieldInfo{
		{Name: "age", Type: reflect.TypeOf(0)},
		{Name: "name", Type: reflect.TypeOf("")},
	}

	tests := []struct {
		name      string
		provider  string
		op        expr.Operator
		right     *expr.Expression
		wantSQL   []string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "equals",
			provider:  "postgresql",
			op:        expr.Equals,
			right:     &expr.Expression{Op: expr.Literal, Left: "john"},
			wantSQL:   []string{`"name"`, "="},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "greater than",
			provider:  "postgresql",
			op:        expr.Greater,
			right:     &expr.Expression{Op: expr.Literal, Left: 25},
			wantSQL:   []string{`"age"`, ">"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "less than",
			provider:  "postgresql",
			op:        expr.Less,
			right:     &expr.Expression{Op: expr.Literal, Left: 65},
			wantSQL:   []string{`"age"`, "<"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "greater or equal",
			provider:  "postgresql",
			op:        expr.GreaterEq,
			right:     &expr.Expression{Op: expr.Literal, Left: 25},
			wantSQL:   []string{`"age"`, ">="},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "less or equal",
			provider:  "postgresql",
			op:        expr.LessEq,
			right:     &expr.Expression{Op: expr.Literal, Left: 65},
			wantSQL:   []string{`"age"`, "<="},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "equals null",
			provider:  "postgresql",
			op:        expr.Equals,
			right:     &expr.Expression{Op: expr.Literal, Left: "null"},
			wantSQL:   []string{`"name"`, "IS NULL"},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:     "greater than null (error)",
			provider: "postgresql",
			op:       expr.Greater,
			right:    &expr.Expression{Op: expr.Literal, Left: "null"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := NewSQLDriver(fields, tt.provider)
			if err != nil {
				t.Fatalf("NewSQLDriver() error = %v", err)
			}
			var left expr.Column
			if strings.Contains(tt.name, "age") || tt.op == expr.Greater || tt.op == expr.Less || tt.op == expr.GreaterEq || tt.op == expr.LessEq {
				left = expr.Column("age")
			} else {
				left = expr.Column("name")
			}
			e := &expr.Expression{
				Op:    tt.op,
				Left:  left,
				Right: tt.right,
			}
			sql, params, err := driver.renderComparison(e)
			if (err != nil) != tt.wantErr {
				t.Errorf("renderComparison() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, want := range tt.wantSQL {
					if !strings.Contains(sql, want) {
						t.Errorf("renderComparison() sql = %v, want to contain %v", sql, want)
					}
				}
				if len(params) != tt.wantCount {
					t.Errorf("renderComparison() params count = %v, want %v", len(params), tt.wantCount)
				}
			}
		})
	}
}

func TestSQLDriver_RenderBinary(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
		{Name: "email", Type: reflect.TypeOf("")},
	}

	tests := []struct {
		name      string
		provider  string
		op        expr.Operator
		left      *expr.Expression
		right     *expr.Expression
		wantSQL   []string
		wantCount int
		wantErr   bool
	}{
		{
			name:     "AND",
			provider: "postgresql",
			op:       expr.And,
			left: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			right: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("email"),
				Right: &expr.Expression{Op: expr.Literal, Left: "test@example.com"},
			},
			wantSQL:   []string{"AND"},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:     "OR",
			provider: "postgresql",
			op:       expr.Or,
			left: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			right: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "jane"},
			},
			wantSQL:   []string{"OR"},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:     "Must",
			provider: "postgresql",
			op:       expr.Must,
			left: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			right:     nil,
			wantSQL:   []string{`"name"`},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "MustNot",
			provider: "postgresql",
			op:       expr.MustNot,
			left: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			right:     nil,
			wantSQL:   []string{"NOT"},
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := NewSQLDriver(fields, tt.provider)
			if err != nil {
				t.Fatalf("NewSQLDriver() error = %v", err)
			}
			e := &expr.Expression{
				Op:    tt.op,
				Left:  tt.left,
				Right: tt.right,
			}
			sql, params, err := driver.renderBinary(e)
			if (err != nil) != tt.wantErr {
				t.Errorf("renderBinary() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, want := range tt.wantSQL {
					if !strings.Contains(sql, want) {
						t.Errorf("renderBinary() sql = %v, want to contain %v", sql, want)
					}
				}
				if len(params) != tt.wantCount {
					t.Errorf("renderBinary() params count = %v, want %v", len(params), tt.wantCount)
				}
			}
		})
	}
}

func TestSQLDriver_RenderRange(t *testing.T) {
	fields := []FieldInfo{
		{Name: "age", Type: reflect.TypeOf(0)},
		{Name: "date", Type: reflect.TypeOf("")},
	}

	tests := []struct {
		name      string
		provider  string
		rangeExpr *expr.RangeBoundary
		wantSQL   []string
		wantCount int
		wantErr   bool
	}{
		{
			name:     "inclusive range",
			provider: "postgresql",
			rangeExpr: &expr.RangeBoundary{
				Min:       &expr.Expression{Op: expr.Literal, Left: "25"},
				Max:       &expr.Expression{Op: expr.Literal, Left: "65"},
				Inclusive: true,
			},
			wantSQL:   []string{"BETWEEN"},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:     "exclusive range",
			provider: "postgresql",
			rangeExpr: &expr.RangeBoundary{
				Min:       &expr.Expression{Op: expr.Literal, Left: "25"},
				Max:       &expr.Expression{Op: expr.Literal, Left: "65"},
				Inclusive: false,
			},
			wantSQL:   []string{">", "<"},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:     "open-ended min (inclusive)",
			provider: "postgresql",
			rangeExpr: &expr.RangeBoundary{
				Min:       &expr.Expression{Op: expr.Literal, Left: "*"},
				Max:       &expr.Expression{Op: expr.Literal, Left: "65"},
				Inclusive: true,
			},
			wantSQL:   []string{"<="},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "open-ended min (exclusive)",
			provider: "postgresql",
			rangeExpr: &expr.RangeBoundary{
				Min:       &expr.Expression{Op: expr.Literal, Left: "*"},
				Max:       &expr.Expression{Op: expr.Literal, Left: "65"},
				Inclusive: false,
			},
			wantSQL:   []string{"<"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "open-ended max (inclusive)",
			provider: "postgresql",
			rangeExpr: &expr.RangeBoundary{
				Min:       &expr.Expression{Op: expr.Literal, Left: "25"},
				Max:       &expr.Expression{Op: expr.Literal, Left: "*"},
				Inclusive: true,
			},
			wantSQL:   []string{">="},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "open-ended max (exclusive)",
			provider: "postgresql",
			rangeExpr: &expr.RangeBoundary{
				Min:       &expr.Expression{Op: expr.Literal, Left: "25"},
				Max:       &expr.Expression{Op: expr.Literal, Left: "*"},
				Inclusive: false,
			},
			wantSQL:   []string{">"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "both wildcards (error)",
			provider: "postgresql",
			rangeExpr: &expr.RangeBoundary{
				Min:       &expr.Expression{Op: expr.Literal, Left: "*"},
				Max:       &expr.Expression{Op: expr.Literal, Left: "*"},
				Inclusive: true,
			},
			wantErr: true,
		},
		{
			name:      "invalid range expression (error)",
			provider:  "postgresql",
			rangeExpr: nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := NewSQLDriver(fields, tt.provider)
			if err != nil {
				t.Fatalf("NewSQLDriver() error = %v", err)
			}
			var e *expr.Expression
			if tt.rangeExpr == nil {
				e = &expr.Expression{
					Op:    expr.Range,
					Left:  expr.Column("age"),
					Right: nil,
				}
			} else {
				e = &expr.Expression{
					Op:    expr.Range,
					Left:  expr.Column("age"),
					Right: tt.rangeExpr,
				}
			}
			sql, params, err := driver.renderRange(e)
			if (err != nil) != tt.wantErr {
				t.Errorf("renderRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				for _, want := range tt.wantSQL {
					if !strings.Contains(sql, want) {
						t.Errorf("renderRange() sql = %v, want to contain %v", sql, want)
					}
				}
				if len(params) != tt.wantCount {
					t.Errorf("renderRange() params count = %v, want %v", len(params), tt.wantCount)
				}
			}
		})
	}
}

func TestSQLDriver_SerializeColumn(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
		{Name: "metadata", Type: reflect.TypeOf(map[string]interface{}{})},
	}
	driver, err := NewSQLDriver(fields, "postgresql")
	if err != nil {
		t.Fatalf("NewSQLDriver() error = %v", err)
	}

	tests := []struct {
		name      string
		input     any
		wantSQL   string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "simple column",
			input:     expr.Column("name"),
			wantSQL:   `"name"`,
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "JSON syntax column",
			input:     expr.Column("metadata->>'key'"),
			wantSQL:   "metadata->>'key'",
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "string column",
			input:     "name",
			wantSQL:   `"name"`,
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "JSON syntax string",
			input:     "metadata->>'key'",
			wantSQL:   "metadata->>'key'",
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "expression with Literal column",
			input:     &expr.Expression{Op: expr.Literal, Left: expr.Column("name")},
			wantSQL:   `"name"`,
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:    "invalid type",
			input:   123,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := driver.serializeColumn(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("serializeColumn() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if !strings.Contains(sql, tt.wantSQL) {
					t.Errorf("serializeColumn() sql = %v, want to contain %v", sql, tt.wantSQL)
				}
				if len(params) != tt.wantCount {
					t.Errorf("serializeColumn() params count = %v, want %v", len(params), tt.wantCount)
				}
			}
		})
	}
}

func TestSQLDriver_SerializeValue(t *testing.T) {
	fields := []FieldInfo{{Name: "name", Type: reflect.TypeOf("")}}
	driver, err := NewSQLDriver(fields, "postgresql")
	if err != nil {
		t.Fatalf("NewSQLDriver() error = %v", err)
	}

	tests := []struct {
		name      string
		input     any
		wantSQL   string
		wantValue string
		wantErr   bool
	}{
		{
			name:      "string value",
			input:     "john",
			wantSQL:   "?",
			wantValue: "john",
			wantErr:   false,
		},
		{
			name:      "string with wildcards",
			input:     "john*",
			wantSQL:   "?",
			wantValue: "john%",
			wantErr:   false,
		},
		{
			name:      "literal expression",
			input:     &expr.Expression{Op: expr.Literal, Left: "test"},
			wantSQL:   "?",
			wantValue: "test",
			wantErr:   false,
		},
		{
			name:      "wild expression",
			input:     &expr.Expression{Op: expr.Wild, Left: "test*"},
			wantSQL:   "?",
			wantValue: "test%",
			wantErr:   false,
		},
		{
			name:    "nil value (error)",
			input:   nil,
			wantErr: true,
		},
		{
			name:      "integer value",
			input:     42,
			wantSQL:   "?",
			wantValue: "42",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := driver.serializeValue(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("serializeValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if sql != tt.wantSQL {
					t.Errorf("serializeValue() sql = %v, want %v", sql, tt.wantSQL)
				}
				if len(params) != 1 {
					t.Errorf("serializeValue() params count = %v, want 1", len(params))
					return
				}
				gotValue := fmt.Sprintf("%v", params[0])
				if tt.wantValue != "" && gotValue != tt.wantValue {
					t.Errorf("serializeValue() param value = %v, want %v", gotValue, tt.wantValue)
				}
			}
		})
	}
}

func TestSQLDriver_FormatFieldName(t *testing.T) {
	jsonbType := reflect.TypeOf(map[string]interface{}{})
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
		{Name: "metadata", Type: jsonbType},
	}

	tests := []struct {
		name     string
		provider string
		field    string
		want     string
	}{
		{
			name:     "postgresql JSON field",
			provider: "postgresql",
			field:    "metadata.key",
			want:     "metadata->>'key'",
		},
		{
			name:     "mysql JSON field",
			provider: "mysql",
			field:    "metadata.key",
			want:     "JSON_UNQUOTE(JSON_EXTRACT(metadata, '$.key'))",
		},
		{
			name:     "sqlite JSON field",
			provider: "sqlite",
			field:    "metadata.key",
			want:     "JSON_EXTRACT(metadata, '$.key')",
		},
		{
			name:     "simple field (no dot)",
			provider: "postgresql",
			field:    "name",
			want:     "name",
		},
		{
			name:     "non-JSONB field with dot (no conversion)",
			provider: "postgresql",
			field:    "name.subfield",
			want:     "name.subfield",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := NewSQLDriver(fields, tt.provider)
			if err != nil {
				t.Fatalf("NewSQLDriver() error = %v", err)
			}
			got := driver.formatFieldName(tt.field)
			if string(got) != tt.want {
				t.Errorf("formatFieldName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertWildcards(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no wildcards",
			input: "john",
			want:  "john",
		},
		{
			name:  "single *",
			input: "john*",
			want:  "john%",
		},
		{
			name:  "single ?",
			input: "jo?n",
			want:  "jo_n",
		},
		{
			name:  "multiple *",
			input: "*john*",
			want:  "%john%",
		},
		{
			name:  "multiple ?",
			input: "j??n",
			want:  "j__n",
		},
		{
			name:  "mixed wildcards",
			input: "j*?n",
			want:  "j%_n",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only wildcards",
			input: "***",
			want:  "%%%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertWildcards(tt.input)
			if got != tt.want {
				t.Errorf("convertWildcards() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsJSONSyntax(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "PostgreSQL JSONB operator",
			input: "metadata->>'key'",
			want:  true,
		},
		{
			name:  "MySQL JSON_EXTRACT",
			input: "JSON_EXTRACT(column, '$.field')",
			want:  true,
		},
		{
			name:  "MySQL JSON_UNQUOTE",
			input: "JSON_UNQUOTE(JSON_EXTRACT(column, '$.field'))",
			want:  true,
		},
		{
			name:  "SQLite JSON_EXTRACT",
			input: "JSON_EXTRACT(column, '$.field')",
			want:  true,
		},
		{
			name:  "regular column",
			input: "name",
			want:  false,
		},
		{
			name:  "quoted column",
			input: `"name"`,
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isJSONSyntax(tt.input)
			if got != tt.want {
				t.Errorf("isJSONSyntax() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNullValue(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  bool
	}{
		{
			name:  "null string (lowercase)",
			input: "null",
			want:  true,
		},
		{
			name:  "NULL string (uppercase)",
			input: "NULL",
			want:  true,
		},
		{
			name:  "Null string (mixed case)",
			input: "Null",
			want:  true,
		},
		{
			name:  "null in literal expression",
			input: &expr.Expression{Op: expr.Literal, Left: "null"},
			want:  true,
		},
		{
			name:  "empty string",
			input: "",
			want:  false,
		},
		{
			name:  "nil value",
			input: nil,
			want:  false,
		},
		{
			name:  "regular string",
			input: "john",
			want:  false,
		},
		{
			name:  "nil string",
			input: "nil",
			want:  false,
		},
		{
			name:  "empty string",
			input: "empty",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNullValue(tt.input)
			if got != tt.want {
				t.Errorf("isNullValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertToPostgresPlaceholders(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single placeholder",
			input: "SELECT * FROM users WHERE name = ?",
			want:  "SELECT * FROM users WHERE name = $1",
		},
		{
			name:  "multiple placeholders",
			input: "SELECT * FROM users WHERE name = ? AND age = ?",
			want:  "SELECT * FROM users WHERE name = $1 AND age = $2",
		},
		{
			name:  "no placeholders",
			input: "SELECT * FROM users",
			want:  "SELECT * FROM users",
		},
		{
			name:  "many placeholders",
			input: "? ? ? ? ?",
			want:  "$1 $2 $3 $4 $5",
		},
		{
			name:  "placeholder in string literal (should still convert)",
			input: "SELECT '?' FROM users WHERE name = ?",
			want:  "SELECT '$1' FROM users WHERE name = $2",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertToPostgresPlaceholders(tt.input)
			if got != tt.want {
				t.Errorf("convertToPostgresPlaceholders() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLDriver_ProcessJSONFields(t *testing.T) {
	jsonbType := reflect.TypeOf(map[string]interface{}{})
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
		{Name: "metadata", Type: jsonbType},
	}

	tests := []struct {
		name     string
		provider string
		expr     *expr.Expression
		check    func(t *testing.T, expr *expr.Expression)
	}{
		{
			name:     "postgresql JSON field conversion",
			provider: "postgresql",
			expr: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("metadata.key"),
				Right: &expr.Expression{Op: expr.Literal, Left: "value"},
			},
			check: func(t *testing.T, e *expr.Expression) {
				if col, ok := e.Left.(expr.Column); ok {
					if !strings.Contains(string(col), "->>'") {
						t.Errorf("expected PostgreSQL JSON syntax, got %v", col)
					}
				}
			},
		},
		{
			name:     "mysql JSON field conversion",
			provider: "mysql",
			expr: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("metadata.key"),
				Right: &expr.Expression{Op: expr.Literal, Left: "value"},
			},
			check: func(t *testing.T, e *expr.Expression) {
				if col, ok := e.Left.(expr.Column); ok {
					if !strings.Contains(string(col), "JSON_EXTRACT") {
						t.Errorf("expected MySQL JSON syntax, got %v", col)
					}
				}
			},
		},
		{
			name:     "nested expression",
			provider: "postgresql",
			expr: &expr.Expression{
				Op: expr.And,
				Left: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("metadata.key"),
					Right: &expr.Expression{Op: expr.Literal, Left: "value"},
				},
				Right: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("name"),
					Right: &expr.Expression{Op: expr.Literal, Left: "john"},
				},
			},
			check: func(t *testing.T, e *expr.Expression) {
				if leftExpr, ok := e.Left.(*expr.Expression); ok {
					if col, ok := leftExpr.Left.(expr.Column); ok {
						if !strings.Contains(string(col), "->>'") {
							t.Errorf("expected PostgreSQL JSON syntax in nested expression, got %v", col)
						}
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := NewSQLDriver(fields, tt.provider)
			if err != nil {
				t.Fatalf("NewSQLDriver() error = %v", err)
			}
			driver.processJSONFields(tt.expr)
			if tt.check != nil {
				tt.check(t, tt.expr)
			}
		})
	}
}

func TestSQLDriver_RenderParamInternal(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
	}
	driver, err := NewSQLDriver(fields, "postgresql")
	if err != nil {
		t.Fatalf("NewSQLDriver() error = %v", err)
	}

	tests := []struct {
		name      string
		expr      *expr.Expression
		wantSQL   []string
		wantCount int
		wantErr   bool
	}{
		{
			name: "Like operator",
			expr: &expr.Expression{
				Op:    expr.Like,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			wantSQL:   []string{"ILIKE"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "Fuzzy operator",
			expr: &expr.Expression{
				Op: expr.Fuzzy,
				Left: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("name"),
					Right: &expr.Expression{Op: expr.Literal, Left: "roam"},
				},
			},
			wantSQL:   []string{"similarity"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "Boost operator (error)",
			expr: &expr.Expression{
				Op:    expr.Boost,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			wantErr: true,
		},
		{
			name: "Range operator",
			expr: &expr.Expression{
				Op:   expr.Range,
				Left: expr.Column("name"),
				Right: &expr.RangeBoundary{
					Min:       &expr.Expression{Op: expr.Literal, Left: "a"},
					Max:       &expr.Expression{Op: expr.Literal, Left: "z"},
					Inclusive: true,
				},
			},
			wantSQL:   []string{"BETWEEN"},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:    "nil expression",
			expr:    nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, params, err := driver.renderParamInternal(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("renderParamInternal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if tt.expr == nil {
					if sql != "" {
						t.Errorf("renderParamInternal() sql = %v, want empty string", sql)
					}
					return
				}
				for _, want := range tt.wantSQL {
					if !strings.Contains(sql, want) {
						t.Errorf("renderParamInternal() sql = %v, want to contain %v", sql, want)
					}
				}
				if len(params) != tt.wantCount {
					t.Errorf("renderParamInternal() params count = %v, want %v", len(params), tt.wantCount)
				}
			}
		})
	}
}

func TestSQLDriver_ProviderSpecific(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
		{Name: "metadata", Type: reflect.TypeOf(map[string]interface{}{})},
	}

	tests := []struct {
		name      string
		provider  string
		expr      *expr.Expression
		wantSQL   []string
		wantCount int
		checkFunc func(t *testing.T, sql string, params []any)
	}{
		{
			name:     "postgresql placeholder format",
			provider: "postgresql",
			expr: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			wantSQL:   []string{"?"},
			wantCount: 1,
			checkFunc: func(t *testing.T, sql string, params []any) {
				if !strings.Contains(sql, "?") {
					t.Errorf("expected ? placeholder, got %v", sql)
				}
			},
		},
		{
			name:     "mysql placeholder format",
			provider: "mysql",
			expr: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			wantSQL:   []string{"?"},
			wantCount: 1,
			checkFunc: func(t *testing.T, sql string, params []any) {
				if !strings.Contains(sql, "?") {
					t.Errorf("expected MySQL placeholder (?), got %v", sql)
				}
			},
		},
		{
			name:     "sqlite placeholder format",
			provider: "sqlite",
			expr: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "john"},
			},
			wantSQL:   []string{"?"},
			wantCount: 1,
			checkFunc: func(t *testing.T, sql string, params []any) {
				if !strings.Contains(sql, "?") {
					t.Errorf("expected SQLite placeholder (?), got %v", sql)
				}
			},
		},
		{
			name:     "postgresql JSON field",
			provider: "postgresql",
			expr: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("metadata.key"),
				Right: &expr.Expression{Op: expr.Literal, Left: "value"},
			},
			wantSQL:   []string{"metadata->>'key'"},
			wantCount: 1,
			checkFunc: nil,
		},
		{
			name:     "mysql JSON field",
			provider: "mysql",
			expr: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("metadata.key"),
				Right: &expr.Expression{Op: expr.Literal, Left: "value"},
			},
			wantSQL:   []string{"JSON_EXTRACT"},
			wantCount: 1,
			checkFunc: nil,
		},
		{
			name:     "sqlite JSON field",
			provider: "sqlite",
			expr: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("metadata.key"),
				Right: &expr.Expression{Op: expr.Literal, Left: "value"},
			},
			wantSQL:   []string{"JSON_EXTRACT"},
			wantCount: 1,
			checkFunc: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := NewSQLDriver(fields, tt.provider)
			if err != nil {
				t.Fatalf("NewSQLDriver() error = %v", err)
			}
			sql, params, err := driver.RenderParam(tt.expr)
			if err != nil {
				t.Fatalf("RenderParam() error = %v", err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("RenderParam() sql = %v, want to contain %v", sql, want)
				}
			}
			if len(params) != tt.wantCount {
				t.Errorf("RenderParam() params count = %v, want %v", len(params), tt.wantCount)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, sql, params)
			}
		})
	}
}
