package lucene

import (
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Adding a database must be one edit. This pins the registry as the only place
// a provider name is enumerated: the allowlist that used to live in
// validateProvider is gone, so an unregistered name can only fail here.
func TestDialectRegistry(t *testing.T) {
	for _, name := range []string{"postgresql", "mysql", "sqlite"} {
		d, err := lookupDialect(name)
		if err != nil {
			t.Fatalf("lookupDialect(%q): %v", name, err)
		}
		if d.Name() != name {
			t.Errorf("dialect %q reports Name() = %q", name, d.Name())
		}
	}

	if _, err := lookupDialect("oracle"); err == nil {
		t.Error("lookupDialect(oracle) returned no error")
	} else if !strings.Contains(err.Error(), "postgresql") {
		t.Errorf("error should list the supported providers, got: %v", err)
	}
}

// NewSQLDriver must reject an unknown provider at construction rather than at
// the first query that happens to hit an unimplemented branch.
func TestNewSQLDriverRejectsUnknownProvider(t *testing.T) {
	if _, err := NewSQLDriver(nil, "oracle"); err == nil {
		t.Fatal("NewSQLDriver(oracle) returned no error")
	}
	if _, err := NewSQLDriver(nil, "postgresql"); err != nil {
		t.Fatalf("NewSQLDriver(postgresql): %v", err)
	}
}

// Every dialect must answer every operation with something real.
//
// This is the guard the old switch statements could not provide. Two of them
// fell through silently rather than erroring: identifier quoting defaulted to
// double quotes for any non-MySQL provider, and JSON subfield extraction
// returned the bare column name in its default branch. Both produced a query
// that ran and returned wrong rows. A dialect that ignores its argument fails
// here instead.
func TestEveryDialectImplementsEveryOperation(t *testing.T) {
	for _, name := range dialectNames() {
		d, err := lookupDialect(name)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) {
			if got := d.QuoteIdent("col"); got == "col" || !strings.Contains(got, "col") {
				t.Errorf("QuoteIdent(col) = %q; must quote the identifier", got)
			}
			if got := d.JSONExtract("col", "sub"); !strings.Contains(got, "sub") || got == "col" {
				t.Errorf("JSONExtract(col, sub) = %q; must reference the subfield", got)
			}
			if got := d.ScalarLike("a", "?"); !strings.Contains(got, "?") {
				t.Errorf("ScalarLike(a, ?) = %q; must reference the parameter", got)
			}
			if got := d.ArrayWildcard("col"); !strings.Contains(got, "?") || !strings.Contains(got, "col") {
				t.Errorf("ArrayWildcard(col) = %q; must reference the column and the parameter", got)
			}
			if got := d.ArrayContains("col"); !strings.Contains(got, "?") || !strings.Contains(got, "col") {
				t.Errorf("ArrayContains(col) = %q; must reference the column and the parameter", got)
			}
			// EncodeElement must produce something derived from the value for
			// each type in ElemValue's closed set. A dialect that discarded
			// the value would otherwise pass this guard.
			for _, v := range []ElemValue{
				{Kind: reflect.String, Val: "golang"},
				{Kind: reflect.Int, Val: int64(5)},
				{Kind: reflect.Float64, Val: 1.5},
				{Kind: reflect.Bool, Val: true},
			} {
				got, err := d.EncodeElement(v)
				if err != nil {
					t.Errorf("EncodeElement(%#v): %v", v, err)
					continue
				}
				if got == nil {
					t.Errorf("EncodeElement(%#v) returned nil", v)
					continue
				}
				// Whatever the shape, the value must survive into it.
				rendered := fmt.Sprintf("%v", got)
				if valuer, ok := got.(driver.Valuer); ok {
					dv, err := valuer.Value()
					if err != nil {
						t.Errorf("EncodeElement(%#v).Value(): %v", v, err)
						continue
					}
					rendered = fmt.Sprintf("%v", dv)
				}
				if !strings.Contains(rendered, fmt.Sprintf("%v", v.Val)) {
					t.Errorf("EncodeElement(%#v) = %q, which does not carry the value", v, rendered)
				}
			}
			// Fuzzy may legitimately be unsupported, but it must say so rather
			// than silently returning something that ignores the term.
			if sql, err := d.Fuzzy("col", "?"); err == nil && !strings.Contains(sql, "?") {
				t.Errorf("Fuzzy returned nil error and %q, which ignores the term", sql)
			}
		})
	}
}

// Identifier quoting is the operation where a wrong answer is silent. Without
// ANSI_QUOTES, MySQL reads "name" as a string literal, so a double-quoted
// identifier becomes a constant and the predicate compares the column name to
// itself instead of reading the column.
func TestDialectQuoteIdent(t *testing.T) {
	tests := []struct{ provider, in, want string }{
		{"postgresql", "name", `"name"`},
		{"postgresql", `we"ird`, `"we""ird"`},
		{"sqlite", "name", `"name"`},
		{"sqlite", `we"ird`, `"we""ird"`},
		{"mysql", "name", "`name`"},
		{"mysql", "we`ird", "`we``ird`"},
	}
	for _, tt := range tests {
		d, err := lookupDialect(tt.provider)
		if err != nil {
			t.Fatal(err)
		}
		if got := d.QuoteIdent(tt.in); got != tt.want {
			t.Errorf("%s.QuoteIdent(%q) = %q, want %q", tt.provider, tt.in, got, tt.want)
		}
	}
}

// The JSON subfield default branch used to return the bare column name, which
// meant a provider without an implementation silently lost the extraction and
// filtered on the whole document instead of the field.
func TestDialectJSONExtract(t *testing.T) {
	tests := []struct{ provider, want string }{
		{"postgresql", `"labels"->>'category'`},
		{"mysql", "JSON_UNQUOTE(JSON_EXTRACT(`labels`, '$.category'))"},
		{"sqlite", `JSON_EXTRACT("labels", '$.category')`},
	}
	for _, tt := range tests {
		d, err := lookupDialect(tt.provider)
		if err != nil {
			t.Fatal(err)
		}
		base := d.QuoteIdent("labels")
		if got := d.JSONExtract(base, "category"); got != tt.want {
			t.Errorf("%s.JSONExtract = %q, want %q", tt.provider, got, tt.want)
		}
	}
}

// The generated Postgres SQL must carry no cast at all.
//
// Binding the whole array as ONE parameter lets Postgres infer the element
// type from the column. The older ARRAY[?] form could not: Postgres resolves
// the array constructor to text[] at parse time, before it knows the
// parameter's type, which is why that form needed an explicit ::type[] cast
// naming the column's exact element type.
func TestPostgresArrayContainsHasNoCast(t *testing.T) {
	p, err := NewParser(TypedDoc{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	for _, filter := range []string{"nums:5", "rates:1.5", "flags:true"} {
		where, params, err := p.ParseToSQL(filter, "postgresql")
		if err != nil {
			t.Fatalf("ParseToSQL(%q): %v", filter, err)
		}
		if strings.Contains(where, "::") {
			t.Errorf("ParseToSQL(%q) = %q; must not contain a cast", filter, where)
		}
		if strings.Contains(where, "ARRAY[") {
			t.Errorf("ParseToSQL(%q) = %q; ARRAY[?] cannot infer and must not be used", filter, where)
		}
		if len(params) != 1 {
			t.Fatalf("ParseToSQL(%q) produced %d params, want 1", filter, len(params))
		}
		if _, ok := params[0].(driver.Valuer); !ok {
			t.Errorf("param is %T; must be a driver.Valuer so GORM does not expand it as a slice", params[0])
		}
		if reflect.TypeOf(params[0]).Kind() == reflect.Slice {
			t.Errorf("param is a bare %T; GORM would expand it into multiple placeholders", params[0])
		}
	}
}

// The array literal must survive values that array syntax treats specially.
// An unquoted NULL becomes SQL NULL, an unescaped comma splits one element
// into two, and an unescaped brace or quote is a syntax error.
func TestPGArrayLiteralEscaping(t *testing.T) {
	tests := []struct{ in, want string }{
		{"golang", `{"golang"}`},
		{"two words", `{"two words"}`},
		{"a,b", `{"a,b"}`},
		{"{brace}", `{"{brace}"}`},
		{`has"quote`, `{"has\"quote"}`},
		{`back\slash`, `{"back\\slash"}`},
		{"NULL", `{"NULL"}`},
		{"", `{""}`},
	}
	for _, tt := range tests {
		got, err := pgArrayLiteral{tt.in}.Value()
		if err != nil {
			t.Fatalf("Value(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("pgArrayLiteral{%q}.Value() = %q, want %q", tt.in, got, tt.want)
		}
	}

	// Non-strings format bare — quoting them would make them JSON strings.
	for _, tt := range []struct {
		in   any
		want string
	}{
		{int64(5), "{5}"},
		{int64(-5), "{-5}"},
		{1.5, "{1.5}"},
		{true, "{true}"},
		{false, "{false}"},
	} {
		got, err := pgArrayLiteral{tt.in}.Value()
		if err != nil {
			t.Fatalf("Value(%v): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("pgArrayLiteral{%v}.Value() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// MySQL binds JSON scalar TEXT. Binding a Go bool would be silently WRONG:
// the driver sends true as 1, CAST('1' AS JSON) is the JSON number 1, and
// JSON 1 does not equal JSON true, so a matching row would not match.
func TestMySQLEncodesJSONText(t *testing.T) {
	d, err := lookupDialect("mysql")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		val  ElemValue
		want string
	}{
		{ElemValue{Kind: reflect.Int, Val: int64(5)}, "5"},
		{ElemValue{Kind: reflect.Float64, Val: 1.5}, "1.5"},
		{ElemValue{Kind: reflect.Bool, Val: true}, "true"},
		{ElemValue{Kind: reflect.Bool, Val: false}, "false"},
		{ElemValue{Kind: reflect.String, Val: "golang"}, `"golang"`},
		{ElemValue{Kind: reflect.String, Val: `has"quote`}, `"has\"quote"`},
	}
	for _, tt := range tests {
		got, err := d.EncodeElement(tt.val)
		if err != nil {
			t.Fatalf("EncodeElement(%#v): %v", tt.val, err)
		}
		if got != tt.want {
			t.Errorf("EncodeElement(%#v) = %#v, want %q", tt.val, got, tt.want)
		}
	}
}

// SQLite compares json_each.value against a natively-bound parameter, so the
// value must arrive with its Go type intact rather than as a string.
func TestSQLiteEncodesNativeValue(t *testing.T) {
	d, err := lookupDialect("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		val  ElemValue
		want any
	}{
		{ElemValue{Kind: reflect.Int, Val: int64(5)}, int64(5)},
		{ElemValue{Kind: reflect.Float64, Val: 1.5}, 1.5},
		{ElemValue{Kind: reflect.Bool, Val: true}, true},
		{ElemValue{Kind: reflect.String, Val: "golang"}, "golang"},
	}
	for _, tt := range tests {
		got, err := d.EncodeElement(tt.val)
		if err != nil {
			t.Fatalf("EncodeElement(%#v): %v", tt.val, err)
		}
		if got != tt.want {
			t.Errorf("EncodeElement(%#v) = %#v (%T), want %#v (%T)", tt.val, got, got, tt.want, tt.want)
		}
	}
}

// The JSON base column must be quoted at the point of extraction.
//
// It cannot be quoted later: the rendered expression contains ->> or
// JSON_EXTRACT, so quoteColumn sees isJSONSyntax and passes it through
// untouched. Left bare, a mixed-case column folds on Postgres —
//
//	SELECT Mixed->>'category' FROM t
//	ERROR: column "mixed" does not exist
//
// — and a reserved word is a syntax error on MySQL.
func TestJSONExtractQuotesBaseColumn(t *testing.T) {
	type doc struct {
		Id    string            `json:"id"`
		Mixed map[string]string `json:"Mixed"`
		Order map[string]string `json:"order"` // reserved word in MySQL
	}
	p, err := NewParser(doc{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	tests := []struct {
		provider string
		filter   string
		want     string
	}{
		{"postgresql", "Mixed.category:x", `"Mixed"->>'category'`},
		{"postgresql", "order.category:x", `"order"->>'category'`},
		{"mysql", "Mixed.category:x", "JSON_UNQUOTE(JSON_EXTRACT(`Mixed`, '$.category'))"},
		{"mysql", "order.category:x", "JSON_UNQUOTE(JSON_EXTRACT(`order`, '$.category'))"},
		{"sqlite", "Mixed.category:x", `JSON_EXTRACT("Mixed", '$.category')`},
	}
	for _, tt := range tests {
		t.Run(tt.provider+" "+tt.filter, func(t *testing.T) {
			where, _, err := p.ParseToSQL(tt.filter, tt.provider)
			if err != nil {
				t.Fatalf("ParseToSQL(%q, %s): %v", tt.filter, tt.provider, err)
			}
			if !strings.Contains(where, tt.want) {
				t.Errorf("ParseToSQL(%q, %s) = %q, want it to contain %q", tt.filter, tt.provider, where, tt.want)
			}
		})
	}
}

// Quoting the base must not cause double-quoting: quoteColumn short-circuits
// on isJSONSyntax, which keys off ->> / JSON_EXTRACT / JSON_UNQUOTE and is
// unaffected by the quotes around the base.
func TestJSONExtractIsNotDoubleQuoted(t *testing.T) {
	type doc struct {
		Id     string            `json:"id"`
		Labels map[string]string `json:"labels"`
	}
	p, _ := NewParser(doc{})
	for _, prov := range []string{"postgresql", "mysql", "sqlite"} {
		where, _, err := p.ParseToSQL("labels.category:x", prov)
		if err != nil {
			t.Fatalf("%s: %v", prov, err)
		}
		if strings.Contains(where, `""`) || strings.Contains(where, "``") {
			t.Errorf("%s produced a doubled quote: %q", prov, where)
		}
	}
}
