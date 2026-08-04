package lucene

import (
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
