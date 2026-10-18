package lucene

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
)

func TestNewDynamoDBDriver(t *testing.T) {
	tests := []struct {
		name    string
		fields  []FieldInfo
		want    map[string]FieldInfo
		wantErr bool
	}{
		{
			name:    "empty fields",
			fields:  []FieldInfo{},
			want:    map[string]FieldInfo{},
			wantErr: false,
		},
		{
			name: "single field",
			fields: []FieldInfo{
				{Name: "name", Type: reflect.TypeOf("")},
			},
			want: map[string]FieldInfo{
				"name": {Name: "name", Type: reflect.TypeOf("")},
			},
			wantErr: false,
		},
		{
			name: "multiple fields",
			fields: []FieldInfo{
				{Name: "name", Type: reflect.TypeOf("")},
				{Name: "email", Type: reflect.TypeOf("")},
				{Name: "age", Type: reflect.TypeOf(0)},
			},
			want: map[string]FieldInfo{
				"name":  {Name: "name", Type: reflect.TypeOf("")},
				"email": {Name: "email", Type: reflect.TypeOf("")},
				"age":   {Name: "age", Type: reflect.TypeOf(0)},
			},
			wantErr: false,
		},
		{
			name: "duplicate field names returns error",
			fields: []FieldInfo{
				{Name: "name", Type: reflect.TypeOf("")},
				{Name: "name", Type: reflect.TypeOf(0)},
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "multiple duplicate field names",
			fields: []FieldInfo{
				{Name: "name", Type: reflect.TypeOf("")},
				{Name: "email", Type: reflect.TypeOf("")},
				{Name: "name", Type: reflect.TypeOf(0)},
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, err := NewDynamoDBDriver(tt.fields)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDynamoDBDriver() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewDynamoDBDriver() expected error but got nil")
				}
				if driver != nil {
					t.Errorf("NewDynamoDBDriver() expected nil driver on error, got %v", driver)
				}
				if err != nil && !strings.Contains(err.Error(), "duplicate field name") {
					t.Errorf("NewDynamoDBDriver() error message should contain 'duplicate field name', got: %v", err)
				}
				return
			}
			if driver == nil {
				t.Fatalf("NewDynamoDBDriver() returned nil")
				return
			}
			if len(driver.fields) != len(tt.want) {
				t.Errorf("NewDynamoDBDriver() fields count = %v, want %v", len(driver.fields), len(tt.want))
			}
			for name, wantField := range tt.want {
				gotField, exists := driver.fields[name]
				if !exists {
					t.Errorf("NewDynamoDBDriver() missing field %v", name)
					continue
				}
				if gotField.Name != wantField.Name {
					t.Errorf("NewDynamoDBDriver() field[%v].Name = %v, want %v", name, gotField.Name, wantField.Name)
				}
			}
		})
	}
}

func TestDynamoDBDriver_RenderPartiQL(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
		{Name: "email", Type: reflect.TypeOf("")},
		{Name: "age", Type: reflect.TypeOf(0)},
	}
	driver, err := NewDynamoDBDriver(fields)
	if err != nil {
		t.Fatalf("NewDynamoDBDriver() error = %v", err)
	}

	tests := []struct {
		name      string
		expr      *expr.Expression
		wantSQL   string
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
			wantSQL:   "name",
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
			wantSQL:   "AND",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "OR expression",
			expr: &expr.Expression{
				Op: expr.Or,
				Left: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("name"),
					Right: &expr.Expression{Op: expr.Literal, Left: "john"},
				},
				Right: &expr.Expression{
					Op:    expr.Equals,
					Left:  expr.Column("name"),
					Right: &expr.Expression{Op: expr.Literal, Left: "jane"},
				},
			},
			wantSQL:   "OR",
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "LIKE expression",
			expr: &expr.Expression{
				Op:    expr.Like,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "%john%"},
			},
			wantSQL:   "name",
			wantCount: 1,
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
			partiql, attrs, err := driver.RenderPartiQL(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("RenderPartiQL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if tt.expr == nil {
				if partiql != "" {
					t.Errorf("RenderPartiQL() partiql = %v, want empty string", partiql)
				}
				if len(attrs) != 0 {
					t.Errorf("RenderPartiQL() attrs count = %v, want 0", len(attrs))
				}
				return
			}
			if !strings.Contains(partiql, tt.wantSQL) {
				t.Errorf("RenderPartiQL() partiql = %v, want to contain %v", partiql, tt.wantSQL)
			}
			if len(attrs) != tt.wantCount {
				t.Errorf("RenderPartiQL() attrs count = %v, want %v", len(attrs), tt.wantCount)
			}
			for i, attr := range attrs {
				if attr == nil {
					t.Errorf("RenderPartiQL() attrs[%v] is nil", i)
				}
				if _, ok := attr.(*types.AttributeValueMemberS); !ok {
					t.Errorf("RenderPartiQL() attrs[%v] type = %T, want *types.AttributeValueMemberS", i, attr)
				}
			}
		})
	}
}

func TestDynamoDBDriver_EdgeCases(t *testing.T) {
	fields := []FieldInfo{
		{Name: "name", Type: reflect.TypeOf("")},
	}
	driver, err := NewDynamoDBDriver(fields)
	if err != nil {
		t.Fatalf("NewDynamoDBDriver() error = %v", err)
	}

	tests := []struct {
		name      string
		expr      *expr.Expression
		wantErr   bool
		checkFunc func(t *testing.T, partiql string, attrs []types.AttributeValue)
	}{
		{
			name: "empty string value",
			expr: &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: ""},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, partiql string, attrs []types.AttributeValue) {
				if len(attrs) != 1 {
					t.Errorf("expected 1 attribute, got %d", len(attrs))
				}
			},
		},
		{
			name: "LIKE with empty pattern",
			expr: &expr.Expression{
				Op:    expr.Like,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: ""},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, partiql string, attrs []types.AttributeValue) {
				if !strings.Contains(partiql, "name") {
					t.Errorf("expected partiql to contain 'name', got %v", partiql)
				}
			},
		},
		{
			name: "nested AND with LIKE",
			expr: &expr.Expression{
				Op: expr.And,
				Left: &expr.Expression{
					Op:    expr.Like,
					Left:  expr.Column("name"),
					Right: &expr.Expression{Op: expr.Literal, Left: "%john%"},
				},
				Right: &expr.Expression{
					Op:    expr.Like,
					Left:  expr.Column("name"),
					Right: &expr.Expression{Op: expr.Literal, Left: "jane%"},
				},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, partiql string, attrs []types.AttributeValue) {
				if !strings.Contains(partiql, "AND") {
					t.Errorf("expected partiql to contain 'AND', got %v", partiql)
				}
				if len(attrs) < 2 {
					t.Errorf("expected at least 2 attributes, got %d", len(attrs))
				}
			},
		},
		{
			name: "comparison operators",
			expr: &expr.Expression{
				Op:    expr.Greater,
				Left:  expr.Column("name"),
				Right: &expr.Expression{Op: expr.Literal, Left: "a"},
			},
			wantErr: false,
			checkFunc: func(t *testing.T, partiql string, attrs []types.AttributeValue) {
				if !strings.Contains(partiql, ">") {
					t.Errorf("expected partiql to contain '>', got %v", partiql)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			partiql, attrs, err := driver.RenderPartiQL(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("RenderPartiQL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checkFunc != nil {
				tt.checkFunc(t, partiql, attrs)
			}
		})
	}
}

func ddbArrayFields() []FieldInfo {
	return []FieldInfo{
		{Name: "title", Type: reflect.TypeOf("")},
		{Name: "tags", Type: reflect.TypeOf([]string{})},
	}
}

func TestDynamoDBDriver_ArrayContainment(t *testing.T) {
	d, err := NewDynamoDBDriver(ddbArrayFields())
	if err != nil {
		t.Fatalf("NewDynamoDBDriver: %v", err)
	}

	e := &expr.Expression{
		Op:    expr.Equals,
		Left:  expr.Column("tags"),
		Right: &expr.Expression{Op: expr.Literal, Left: "golang"},
	}

	sql, _, err := d.RenderPartiQL(e)
	if err != nil {
		t.Fatalf("RenderPartiQL: %v", err)
	}
	if !strings.Contains(sql, "contains(") {
		t.Errorf("expected contains() for an array field, got %q", sql)
	}
	if strings.Contains(sql, "tags = ") {
		t.Errorf("array field must not render scalar equality, got %q", sql)
	}
}

// Guards the go-lucene recursion trap: Base.RenderParam recurses through its
// own serializeParams, so a top-level override would miss this nested node.
func TestDynamoDBDriver_ArrayContainmentNestedInAnd(t *testing.T) {
	d, err := NewDynamoDBDriver(ddbArrayFields())
	if err != nil {
		t.Fatalf("NewDynamoDBDriver: %v", err)
	}

	e := &expr.Expression{
		Op: expr.And,
		Left: &expr.Expression{
			Op:    expr.Equals,
			Left:  expr.Column("title"),
			Right: &expr.Expression{Op: expr.Literal, Left: "hello"},
		},
		Right: &expr.Expression{
			Op:    expr.Equals,
			Left:  expr.Column("tags"),
			Right: &expr.Expression{Op: expr.Literal, Left: "golang"},
		},
	}

	sql, _, err := d.RenderPartiQL(e)
	if err != nil {
		t.Fatalf("RenderPartiQL: %v", err)
	}
	if !strings.Contains(sql, "contains(") {
		t.Errorf("nested array equality must still render contains(), got %q", sql)
	}
}

func TestDynamoDBDriver_ArrayWildcardIsError(t *testing.T) {
	d, err := NewDynamoDBDriver(ddbArrayFields())
	if err != nil {
		t.Fatalf("NewDynamoDBDriver: %v", err)
	}

	e := &expr.Expression{
		Op:    expr.Wild,
		Left:  expr.Column("tags"),
		Right: &expr.Expression{Op: expr.Literal, Left: "*go*"},
	}

	_, _, err = d.RenderPartiQL(e)
	if err == nil {
		t.Fatal("expected error for wildcard on a DynamoDB array field, got nil")
	}
	if !strings.Contains(err.Error(), "tags") {
		t.Errorf("error must name the field, got %q", err.Error())
	}
}

func TestDynamoDBDriver_ScalarUnchanged(t *testing.T) {
	d, err := NewDynamoDBDriver(ddbArrayFields())
	if err != nil {
		t.Fatalf("NewDynamoDBDriver: %v", err)
	}

	e := &expr.Expression{
		Op:    expr.Equals,
		Left:  expr.Column("title"),
		Right: &expr.Expression{Op: expr.Literal, Left: "hello"},
	}

	sql, _, err := d.RenderPartiQL(e)
	if err != nil {
		t.Fatalf("RenderPartiQL: %v", err)
	}
	if strings.Contains(sql, "contains(") {
		t.Errorf("scalar field must not use contains(), got %q", sql)
	}
}

// The NOT keyword is a different go-lucene operator from the `-` prefix
// (Not vs MustNot). Only MustNot reached the array interception, so
// `NOT tags:golang` re-emitted scalar equality.
func TestDynamoDBDriver_NotKeywordOnArrayField(t *testing.T) {
	p, err := NewParser(Article{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	tests := []struct {
		name    string
		filter  string
		wantSQL []string
	}{
		{"not keyword", "NOT tags:golang", []string{"NOT (", "contains(tags, ?)"}},
		{"mustnot prefix", "-tags:golang", []string{"NOT (", "contains(tags, ?)"}},
		{"inside a compound expression", "title:hello AND NOT tags:golang", []string{"NOT (", "contains(tags, ?)"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := p.ParseToDynamoDBPartiQL(tt.filter)
			if err != nil {
				t.Fatalf("ParseToDynamoDBPartiQL(%q): %v", tt.filter, err)
			}
			for _, want := range tt.wantSQL {
				if !strings.Contains(sql, want) {
					t.Errorf("partiql %q missing %q", sql, want)
				}
			}
			if strings.Contains(sql, `"tags" = ?`) {
				t.Errorf("array field must not render scalar equality, got %q", sql)
			}
		})
	}
}

// Negation over field:null stays IS NOT NULL rather than NOT (... IS NULL).
func TestDynamoDBDriver_NegatedNullStaysIsNotNull(t *testing.T) {
	p, err := NewParser(Article{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	for _, filter := range []string{"NOT tags:null", "NOT title:null", "-tags:null", "-title:null"} {
		t.Run(filter, func(t *testing.T) {
			sql, params, err := p.ParseToDynamoDBPartiQL(filter)
			if err != nil {
				t.Fatalf("ParseToDynamoDBPartiQL(%q): %v", filter, err)
			}
			if !strings.Contains(sql, "IS NOT NULL") {
				t.Errorf("partiql %q must render IS NOT NULL", sql)
			}
			if strings.Contains(sql, "NOT (") {
				t.Errorf("partiql %q must collapse the negation, not wrap it", sql)
			}
			if len(params) != 0 {
				t.Errorf("a null check binds no params, got %#v", params)
			}
		})
	}
}

// A grouped value list under an array field used to be stringified whole and
// bound as one value ("id:golang OR id:rust"), matching nothing silently.
func TestDynamoDBDriver_ArrayGroupedValues(t *testing.T) {
	p, err := NewParser(Article{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	sql, params, err := p.ParseToDynamoDBPartiQL("tags:(golang OR rust)")
	if err != nil {
		t.Fatalf("ParseToDynamoDBPartiQL: %v", err)
	}
	if strings.Count(sql, "contains(tags, ?)") != 2 {
		t.Errorf("expected one contains() per group member, got %q", sql)
	}
	if !strings.Contains(sql, " OR ") {
		t.Errorf("expected the group to stay disjunctive, got %q", sql)
	}
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %#v", len(params), params)
	}
	for i, want := range []string{"golang", "rust"} {
		got, ok := params[i].(*types.AttributeValueMemberS)
		if !ok || got.Value != want {
			t.Errorf("param %d = %#v, want %q", i, params[i], want)
		}
	}
}

func TestDynamoDBDriver_ArrayGroupedWildcardIsError(t *testing.T) {
	p, err := NewParser(Article{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	_, _, err = p.ParseToDynamoDBPartiQL("tags:(golang* OR rust)")
	if err == nil {
		t.Fatal("expected an error for a wildcard leaf on a DynamoDB array field, got nil")
	}
	if !strings.Contains(err.Error(), "tags") {
		t.Errorf("error must name the field, got %q", err.Error())
	}
}

// PartiQL has no ordering semantics for a list attribute, so these must fail
// closed rather than render a comparison that silently matches nothing —
// mirroring the SQL driver.
func TestDynamoDBDriver_ArrayOrderingAndRangeAreErrors(t *testing.T) {
	p, err := NewParser(Article{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	for _, filter := range []string{"tags:>5", "tags:<5", "tags:>=5", "tags:<=5", "tags:[a TO b]", "tags:{a TO b}"} {
		t.Run(filter, func(t *testing.T) {
			sql, _, err := p.ParseToDynamoDBPartiQL(filter)
			if err == nil {
				t.Fatalf("expected an error for %q on an array field, got %q", filter, sql)
			}
			if !strings.Contains(err.Error(), "tags") {
				t.Errorf("error must name the field, got %q", err.Error())
			}
		})
	}
}

// Containment values must reach DynamoDB as the attribute type they are
// compared against. Binding every value as a string attribute makes numeric
// and boolean array containment silently match nothing.
func TestDynamoDBDriver_TypedArrayContainmentAttributeTypes(t *testing.T) {
	fields := []FieldInfo{
		{Name: "nums", Type: reflect.TypeOf([]int{})},
		{Name: "flags", Type: reflect.TypeOf([]bool{})},
		{Name: "tags", Type: reflect.TypeOf([]string{})},
	}
	d, err := NewDynamoDBDriver(fields)
	if err != nil {
		t.Fatalf("NewDynamoDBDriver: %v", err)
	}

	tests := []struct {
		name  string
		field string
		value string
		check func(types.AttributeValue) bool
		want  string
	}{
		{"int array binds N", "nums", "5", func(v types.AttributeValue) bool {
			n, ok := v.(*types.AttributeValueMemberN)
			return ok && n.Value == "5"
		}, "N=5"},
		{"bool array binds BOOL", "flags", "true", func(v types.AttributeValue) bool {
			b, ok := v.(*types.AttributeValueMemberBOOL)
			return ok && b.Value
		}, "BOOL=true"},
		{"string array still binds S", "tags", "golang", func(v types.AttributeValue) bool {
			s, ok := v.(*types.AttributeValueMemberS)
			return ok && s.Value == "golang"
		}, "S=golang"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &expr.Expression{
				Op:    expr.Equals,
				Left:  expr.Column(tt.field),
				Right: &expr.Expression{Op: expr.Literal, Left: tt.value},
			}
			sql, params, err := d.RenderPartiQL(e)
			if err != nil {
				t.Fatalf("RenderPartiQL: %v", err)
			}
			if !strings.Contains(sql, "contains("+tt.field+", ?)") {
				t.Errorf("sql %q is not a containment test", sql)
			}
			if len(params) != 1 {
				t.Fatalf("expected 1 param, got %d", len(params))
			}
			if !tt.check(params[0]) {
				t.Errorf("param %#v is not %s", params[0], tt.want)
			}
		})
	}
}

// A value that cannot be an element of the field's type is a malformed filter.
func TestDynamoDBDriver_TypedArrayRejectsMistypedValue(t *testing.T) {
	d, err := NewDynamoDBDriver([]FieldInfo{{Name: "nums", Type: reflect.TypeOf([]int{})}})
	if err != nil {
		t.Fatalf("NewDynamoDBDriver: %v", err)
	}

	e := &expr.Expression{
		Op:    expr.Equals,
		Left:  expr.Column("nums"),
		Right: &expr.Expression{Op: expr.Literal, Left: "abc"},
	}
	if _, _, err := d.RenderPartiQL(e); err == nil {
		t.Error("RenderPartiQL returned no error for a non-numeric value on an int array")
	}
}

// DynamoDB distinguishes N, S and BOOL attributes, and a value stored as N
// never equals one bound as S. Deriving the attribute type by re-parsing a
// canonical string was a second source of truth that could disagree with the
// validation that produced it; elemValue carries the type directly.
func TestElemAttributeValueFromElemValue(t *testing.T) {
	tests := []struct {
		name string
		val  elemValue
		want types.AttributeValue
	}{
		{"int", elemValue{Kind: reflect.Int, Val: int64(5)}, &types.AttributeValueMemberN{Value: "5"}},
		{"negative", elemValue{Kind: reflect.Int64, Val: int64(-5)}, &types.AttributeValueMemberN{Value: "-5"}},
		{"uint carried as int64", elemValue{Kind: reflect.Uint64, Val: int64(9223372036854775807)}, &types.AttributeValueMemberN{Value: "9223372036854775807"}},
		{"float", elemValue{Kind: reflect.Float64, Val: 1.5}, &types.AttributeValueMemberN{Value: "1.5"}},
		{"float32 width", elemValue{Kind: reflect.Float32, Val: 2.5}, &types.AttributeValueMemberN{Value: "2.5"}},
		{"bool true", elemValue{Kind: reflect.Bool, Val: true}, &types.AttributeValueMemberBOOL{Value: true}},
		{"bool false", elemValue{Kind: reflect.Bool, Val: false}, &types.AttributeValueMemberBOOL{Value: false}},
		{"string", elemValue{Kind: reflect.String, Val: "golang"}, &types.AttributeValueMemberS{Value: "golang"}},
		// An unlisted element kind stays a string and must bind as S, not N.
		{"unlisted kind", elemValue{Kind: reflect.Struct, Val: "2024-01-01T00:00:00Z"}, &types.AttributeValueMemberS{Value: "2024-01-01T00:00:00Z"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := elemAttributeValue(tt.val)
			if err != nil {
				t.Fatalf("elemAttributeValue(%#v): %v", tt.val, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("elemAttributeValue(%#v) = %#v, want %#v", tt.val, got, tt.want)
			}
		})
	}

	// A Val outside the closed set is a programming error, not a filter error.
	if _, err := elemAttributeValue(elemValue{Kind: reflect.Complex64, Val: complex64(1)}); err == nil {
		t.Error("elemAttributeValue with an unsupported Val returned no error")
	}
}

// gorm tags describe SQL storage only: DynamoDB keeps every json-tagged field.
func TestGormIgnoredFieldsStayFilterableOnDynamoDB(t *testing.T) {
	p, err := NewParser(Order{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	for _, filter := range []string{"note:x", "summary:x"} {
		t.Run(filter, func(t *testing.T) {
			if _, _, err := p.ParseToDynamoDBPartiQL(filter); err != nil {
				t.Errorf("ParseToDynamoDBPartiQL(%q): %v", filter, err)
			}
		})
	}
}

type ddbRule struct {
	Tenant    string            `json:"tenant"`
	ID        string            `json:"id"`
	Priority  int               `json:"priority"`
	Level     uint8             `json:"level"`
	Score     float64           `json:"score"`
	Enabled   bool              `json:"enabled"`
	Retries   *int              `json:"retries"`
	Owner     *string           `json:"owner"`
	CreatedAt time.Time         `json:"created_at"`
	Labels    map[string]string `json:"labels"`
	Tags      []string          `json:"tags"`
}

func ddbStringParams(t *testing.T, params []types.AttributeValue) []string {
	t.Helper()
	out := make([]string, len(params))
	for i, p := range params {
		s, ok := p.(*types.AttributeValueMemberS)
		if !ok {
			t.Fatalf("param %d = %#v; want a string attribute", i, p)
		}
		out[i] = s.Value
	}
	return out
}

// DynamoDB PartiQL has no SIMILAR TO or LIKE. Wildcards used to fall through
// to go-lucene's Postgres rendering, so every wildcard search failed.
func TestDynamoDBDriver_ScalarWildcards(t *testing.T) {
	p, err := NewParser(ddbRule{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	tests := []struct {
		query      string
		wantSQL    string
		wantParams []string
	}{
		{"id:abc*", `begins_with("id", ?)`, []string{"abc"}},
		{"id:connector#c-42*", `begins_with("id", ?)`, []string{"connector#c-42"}},
		{"id:*abc*", `contains("id", ?)`, []string{"abc"}},
		{"id:*", `"id" IS NOT MISSING`, []string{}},
		{"owner:abc*", `begins_with("owner", ?)`, []string{"abc"}},
		{`tenant:"acme" AND id:abc*`, `("tenant" = ?) AND (begins_with("id", ?))`, []string{"acme", "abc"}},
		{"abc*", `(begins_with("tenant", ?)) OR (begins_with("id", ?))`, []string{"abc", "abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			sql, params, err := p.ParseToDynamoDBPartiQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToDynamoDBPartiQL(%q): %v", tt.query, err)
			}
			if !strings.Contains(sql, tt.wantSQL) {
				t.Errorf("partiql = %q; want it to contain %q", sql, tt.wantSQL)
			}
			if strings.Contains(sql, "SIMILAR") || strings.Contains(sql, "LIKE") {
				t.Errorf("partiql = %q; DynamoDB supports neither SIMILAR TO nor LIKE", sql)
			}
			got := ddbStringParams(t, params)
			if strings.Join(got, "|") != strings.Join(tt.wantParams, "|") {
				t.Errorf("params = %q; want %q", got, tt.wantParams)
			}
		})
	}
}

// Patterns PartiQL can't express must fail rather than match the wrong items.
func TestDynamoDBDriver_UnsupportedWildcardsAreErrors(t *testing.T) {
	p, err := NewParser(ddbRule{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	for _, query := range []string{"id:*abc", "id:a*c", "id:a?c", "id:*a*b*", "priority:5*"} {
		t.Run(query, func(t *testing.T) {
			if sql, _, err := p.ParseToDynamoDBPartiQL(query); err == nil {
				t.Fatalf("ParseToDynamoDBPartiQL(%q) = %q; want an error", query, sql)
			}
		})
	}
}

// DynamoDB never matches an N attribute against an S value, so binding every
// scalar as a string made numeric and boolean searches silently return nothing.
func TestDynamoDBDriver_ScalarValuesBindAsFieldType(t *testing.T) {
	p, err := NewParser(ddbRule{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	n := func(v string) types.AttributeValue { return &types.AttributeValueMemberN{Value: v} }
	s := func(v string) types.AttributeValue { return &types.AttributeValueMemberS{Value: v} }
	b := func(v bool) types.AttributeValue { return &types.AttributeValueMemberBOOL{Value: v} }

	tests := []struct {
		query      string
		wantSQL    string
		wantParams []types.AttributeValue
	}{
		{"priority:80", `"priority" = ?`, []types.AttributeValue{n("80")}},
		{"priority:>80", `"priority" > ?`, []types.AttributeValue{n("80")}},
		{"priority:<=80", `"priority" <= ?`, []types.AttributeValue{n("80")}},
		{"level:255", `"level" = ?`, []types.AttributeValue{n("255")}},
		{"retries:3", `"retries" = ?`, []types.AttributeValue{n("3")}},
		{"score:1.5", `"score" = ?`, []types.AttributeValue{n("1.5")}},
		{"enabled:true", `"enabled" = ?`, []types.AttributeValue{b(true)}},
		{"tenant:acme", `"tenant" = ?`, []types.AttributeValue{s("acme")}},
		{"tenant:42", `"tenant" = ?`, []types.AttributeValue{s("42")}},
		{"owner:bob", `"owner" = ?`, []types.AttributeValue{s("bob")}},
		{`created_at:"2024-01-01T00:00:00Z"`, `"created_at" = ?`, []types.AttributeValue{s("2024-01-01T00:00:00Z")}},
		{"priority:[50 TO 100]", `"priority" BETWEEN ? AND ?`, []types.AttributeValue{n("50"), n("100")}},
		{"priority:{50 TO 100}", `"priority" > ? AND "priority" < ?`, []types.AttributeValue{n("50"), n("100")}},
		{"priority:[50 TO *]", `"priority" >= ?`, []types.AttributeValue{n("50")}},
		{"priority:{* TO 100}", `"priority" < ?`, []types.AttributeValue{n("100")}},
		{"id:[a TO m]", `"id" BETWEEN ? AND ?`, []types.AttributeValue{s("a"), s("m")}},
		{"priority:null", `"priority" IS NULL`, []types.AttributeValue{}},
		{"labels.team:core", `"labels"."team" = ?`, []types.AttributeValue{s("core")}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			sql, params, err := p.ParseToDynamoDBPartiQL(tt.query)
			if err != nil {
				t.Fatalf("ParseToDynamoDBPartiQL(%q): %v", tt.query, err)
			}
			if sql != tt.wantSQL {
				t.Errorf("partiql = %q; want %q", sql, tt.wantSQL)
			}
			if len(params) != len(tt.wantParams) {
				t.Fatalf("params = %#v; want %#v", params, tt.wantParams)
			}
			for i := range params {
				if !reflect.DeepEqual(params[i], tt.wantParams[i]) {
					t.Errorf("param %d = %#v; want %#v", i, params[i], tt.wantParams[i])
				}
			}
		})
	}
}

// A value that can't be stored in the field is a malformed query, not one
// that should quietly match nothing.
func TestDynamoDBDriver_ScalarRejectsMistypedValue(t *testing.T) {
	p, err := NewParser(ddbRule{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	for _, query := range []string{
		"priority:abc",
		"priority:1.5",
		"level:256",
		"enabled:maybe",
		"score:NaN",
		"priority:[1 TO abc]",
		"priority:>null",
		"priority:[* TO *]",
	} {
		t.Run(query, func(t *testing.T) {
			if sql, _, err := p.ParseToDynamoDBPartiQL(query); err == nil {
				t.Fatalf("ParseToDynamoDBPartiQL(%q) = %q; want an error", query, sql)
			}
		})
	}
}
