package lucene

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

func init() { registerDialect(postgresDialect{}) }

type postgresDialect struct{}

func (postgresDialect) Name() string { return "postgresql" }

func (postgresDialect) QuoteIdent(col string) string {
	return `"` + strings.ReplaceAll(col, `"`, `""`) + `"`
}

func (postgresDialect) JSONExtract(base, subField string) string {
	// JSONB ->> with the key in single quotes, so single quotes are escaped.
	return fmt.Sprintf("%s->>'%s'", base, escapeJSONPathSegment(subField))
}

func (postgresDialect) ScalarLike(left, right string) string {
	// ILIKE is case-insensitive. A non-text column needs ::text first;
	// a ->> expression is already text.
	if isJSONSyntax(left) {
		return fmt.Sprintf("%s ILIKE %s", left, right)
	}
	return fmt.Sprintf("%s::text ILIKE %s", left, right)
}

func (postgresDialect) ArrayWildcard(col string) string {
	return fmt.Sprintf("EXISTS (SELECT 1 FROM unnest(%s) AS elem WHERE elem ILIKE ?)", col)
}

func (postgresDialect) Fuzzy(col, term string) (string, error) {
	// similarity() comes from the pg_trgm extension. Threshold 0.3: lower
	// matches more.
	const threshold = 0.3
	if isJSONSyntax(col) {
		return fmt.Sprintf("similarity(%s, %s) > %f", col, term, threshold), nil
	}
	return fmt.Sprintf("similarity(%s::text, %s) > %f", col, term, threshold), nil
}

// ArrayContains binds the whole array as ONE parameter, which is what lets
// Postgres infer the element type from the column.
//
// The older ARRAY[?] form could not: Postgres resolves the array constructor
// to text[] at parse time, before it knows the parameter's type, so it needed
// an explicit ::type[] cast naming the column's exact element type. `@>`
// requires exactly matching array types and neither narrowing nor widening
// rescues a mismatch, so that cast had to be maintained per element kind.
//
// The result is deliberately NOT wrapped in COALESCE(..., false). That wrapper
// makes the expression opaque to the planner and costs the GIN index: measured
// on 300k rows, COALESCE(tags @> ?, false) is a sequential scan at 13.8ms while
// the bare form is a bitmap index scan at 0.036ms, for identical rows.
//
// A NULL column yields NULL here rather than false, which is already correct
// for a positive match (NULL is not true, so the row is excluded). Negation is
// where the difference would matter, and renderBinary handles it by wrapping
// the whole negated subtree in IS NOT TRUE — see subtreeHasArrayContainment.
func (postgresDialect) ArrayContains(col string) string {
	return fmt.Sprintf("%s @> ?", col)
}

func (postgresDialect) EncodeElement(v elemValue) (any, error) {
	return pgArrayLiteral{v.Val}, nil
}

// pgArrayLiteral binds one element as a single-element Postgres array literal.
//
// It MUST be a driver.Valuer rather than a Go slice. GORM expands slice
// arguments for IN (?) clauses, rewriting `col @> ?` into `col @> ($1)` and
// then failing to encode ("unable to encode 5 into binary format for _int8").
// GORM checks driver.Valuer before that expansion, so a Valuer passes through
// as a single parameter. That constraint, not Postgres, is why the obvious
// []int64 version does not work.
type pgArrayLiteral struct{ v any }

// pgArrayElemEscaper escapes the two characters that are special inside a
// quoted array element.
var pgArrayElemEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func (p pgArrayLiteral) Value() (driver.Value, error) {
	if s, ok := p.v.(string); ok {
		// Always quote. An unquoted NULL is SQL NULL, an unquoted comma splits
		// the element in two, and braces and quotes are syntax errors.
		return `{"` + pgArrayElemEscaper.Replace(s) + `"}`, nil
	}
	// int64, float64 and bool all format to valid array literal syntax.
	return fmt.Sprintf("{%v}", p.v), nil
}
