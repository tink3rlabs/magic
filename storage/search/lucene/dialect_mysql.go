package lucene

import (
	"fmt"
	"strings"
)

func init() { registerDialect(mysqlDialect{}) }

type mysqlDialect struct{}

func (mysqlDialect) Name() string { return "mysql" }

func (mysqlDialect) QuoteIdent(col string) string {
	// Backticks, not double quotes: without ANSI_QUOTES, MySQL reads "name" as
	// a string literal, so a double-quoted identifier silently becomes a
	// constant and the predicate compares against the column name itself.
	return "`" + strings.ReplaceAll(col, "`", "``") + "`"
}

func (mysqlDialect) JSONExtract(base, subField string) string {
	return fmt.Sprintf("JSON_UNQUOTE(JSON_EXTRACT(%s, '$.%s'))", base, subField)
}

func (mysqlDialect) ScalarLike(left, right string) string {
	return fmt.Sprintf("LOWER(%s) LIKE LOWER(%s)", left, right)
}

func (mysqlDialect) ArrayWildcard(col string) string {
	// JSON_SEARCH compares JSON string scalars under the utf8mb4_bin collation
	// regardless of the column's or session's collation, so an unfolded match
	// here would be case-sensitive: tags:*Go* would not match ["golang"].
	// Folding both the document and the pattern keeps this consistent with the
	// ILIKE and LIKE branches of the other dialects, and with this dialect's
	// own ScalarLike.
	return fmt.Sprintf("JSON_SEARCH(LOWER(CAST(%s AS CHAR)), 'one', LOWER(?)) IS NOT NULL", col)
}

func (mysqlDialect) Fuzzy(col, term string) (string, error) {
	// SOUNDEX is phonetic rather than edit-distance, and is the closest
	// built-in MySQL offers.
	return fmt.Sprintf("SOUNDEX(%s) = SOUNDEX(%s)", col, term), nil
}
