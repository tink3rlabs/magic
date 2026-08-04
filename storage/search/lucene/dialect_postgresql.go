package lucene

import (
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
