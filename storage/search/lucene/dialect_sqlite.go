package lucene

import (
	"fmt"
	"strings"
)

func init() { registerDialect(sqliteDialect{}) }

type sqliteDialect struct{}

func (sqliteDialect) Name() string { return "sqlite" }

func (sqliteDialect) QuoteIdent(col string) string {
	return `"` + strings.ReplaceAll(col, `"`, `""`) + `"`
}

func (sqliteDialect) JSONExtract(base, subField string) string {
	return fmt.Sprintf("JSON_EXTRACT(%s, '$.%s')", base, subField)
}

func (sqliteDialect) ScalarLike(left, right string) string {
	// LIKE is already case-insensitive for ASCII by default.
	return fmt.Sprintf("%s LIKE %s", left, right)
}

func (sqliteDialect) ArrayWildcard(col string) string {
	return fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(%s) WHERE value LIKE ?)", col)
}

func (sqliteDialect) Fuzzy(col, term string) (string, error) {
	return "", fmt.Errorf("fuzzy search (field:term~N) is not supported with SQLite; use wildcards instead (e.g., field:term*)")
}

// ArrayContains compares json_each.value against a natively-bound parameter.
// json_each yields numbers as numbers and booleans as 1/0, which is exactly
// what the driver sends for an int64, float64 or bool, so the older
// json_extract(json(?), '$') re-parsing wrapper is unnecessary.
func (sqliteDialect) ArrayContains(col string) string {
	return fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(%s) WHERE value = ?)", col)
}

// EncodeElement passes the value through: int64, float64, bool and string are
// all valid driver.Value types.
func (sqliteDialect) EncodeElement(v ElemValue) (any, error) { return v.Val, nil }
