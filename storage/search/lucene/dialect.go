package lucene

import (
	"fmt"
	"sort"
	"strings"
)

// Dialect is the set of per-database differences in rendering a Lucene filter
// to SQL.
//
// These differences used to live in switch statements spread through the
// renderer. Adding a database meant finding every one of them by grep, with no
// compile-time help, and two fell through silently rather than erroring:
// identifier quoting tested only for MySQL and defaulted everything else to
// double quotes, and JSON subfield extraction returned the bare column name in
// its default branch. Both produced a query that ran and returned wrong rows.
// Implementing this interface makes the compiler name every operation still
// missing instead.
//
// Methods that return SQL emit `?` placeholders; the caller's driver
// translates them (GORM rewrites `?` to `$N` for Postgres).
type Dialect interface {
	// Name is the provider string callers pass to NewSQLDriver.
	Name() string

	// QuoteIdent quotes a column identifier, escaping the quote character.
	QuoteIdent(col string) string

	// JSONExtract renders access to a subfield of a JSON column. base is an
	// already-quoted column reference; subField has already been validated
	// against jsonSubFieldPattern.
	JSONExtract(base, subField string) string

	// ScalarLike renders case-insensitive pattern matching on a single-valued
	// column. left and right are already-rendered SQL fragments.
	ScalarLike(left, right string) string

	// ArrayWildcard renders a case-insensitive substring match against any
	// element of a multi-valued column. Only called for string arrays.
	ArrayWildcard(col string) string

	// Fuzzy renders approximate matching, or returns an error naming the
	// limitation when the database has no equivalent.
	Fuzzy(col, term string) (string, error)
}

// dialects is the single enumeration of supported providers. Registration
// happens in each dialect file's init, so adding a database is adding a file.
var dialects = map[string]Dialect{}

func registerDialect(d Dialect) {
	if _, dup := dialects[d.Name()]; dup {
		panic(fmt.Sprintf("lucene: duplicate dialect registration for %q", d.Name()))
	}
	dialects[d.Name()] = d
}

// dialectNames returns the supported provider names in sorted order.
func dialectNames() []string {
	names := make([]string, 0, len(dialects))
	for n := range dialects {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// lookupDialect resolves a provider name. It replaces the hardcoded allowlist
// that used to sit in validateProvider, so the registry is the only place a
// provider name appears.
func lookupDialect(name string) (Dialect, error) {
	d, ok := dialects[name]
	if !ok {
		return nil, fmt.Errorf("unsupported SQL provider: %s (supported: %s)",
			name, strings.Join(dialectNames(), ", "))
	}
	return d, nil
}
