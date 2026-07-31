package lucene

import (
	"database/sql"
	"testing"

	"github.com/grindlemire/go-lucene/pkg/lucene/expr"
	_ "github.com/mattn/go-sqlite3"
)

// Article models a table whose tags column holds a JSON array.
type Article struct {
	Id    string   `json:"id"`
	Title string   `json:"title"`
	Tags  []string `json:"tags"`
}

// newExecDB creates an in-memory SQLite database with a known fixture.
//
//	id | title   | tags
//	 1 | hello   | ["golang","gopher"]
//	 2 | world   | ["rust"]
//	 3 | empty   | []
//	 4 | nulltag | NULL
func newExecDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE articles (id TEXT, title TEXT, tags TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO articles VALUES
		('1','hello','["golang","gopher"]'),
		('2','world','["rust"]'),
		('3','empty','[]'),
		('4','nulltag',NULL)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return db
}

// countMatching runs a parsed filter against the fixture and returns the row count.
// This is the whole point of the file: the SQL must actually execute.
func countMatching(t *testing.T, db *sql.DB, filter string) int {
	t.Helper()

	p, err := NewParser(Article{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	where, params, err := p.ParseToSQL(filter, "sqlite")
	if err != nil {
		t.Fatalf("ParseToSQL(%q): %v", filter, err)
	}

	var n int
	query := "SELECT count(*) FROM articles WHERE " + where
	if err := db.QueryRow(query, params...).Scan(&n); err != nil {
		t.Fatalf("executing %q (from filter %q): %v", query, filter, err)
	}
	return n
}

func TestExecuted_ArrayContainment(t *testing.T) {
	db := newExecDB(t)

	tests := []struct {
		name   string
		filter string
		want   int
	}{
		{"containment matches one row", "tags:golang", 1},
		{"containment matches other row", "tags:rust", 1},
		{"containment misses absent value", "tags:python", 0},
		{"scalar equality still works", "title:hello", 1},
		{"has-value matches non-null including empty array", "tags:*", 3},
		{"wildcard matches per element", "tags:*go*", 1},
		{"wildcard misses absent pattern", "tags:*zzz*", 0},
		{"grouped values", "tags:(golang OR rust)", 2},
		{"grouped wildcard leaf", "tags:(golang* OR rust)", 2},
		{"grouped wildcard leaf alone", "tags:(gop* OR zzz)", 1},
		{"null check", "tags:null", 1},
		{"boolean composition", "title:hello AND tags:golang", 1},
		{"boolean composition no match", "title:world AND tags:golang", 0},

		// The NOT keyword is a different operator from the `-` prefix and used
		// to bypass array handling entirely. Rows 2, 3 and 4 (rust, [], NULL)
		// are the non-golang rows — the NULL row included, which is what the
		// COALESCE/EXISTS shaping is there to guarantee.
		{"NOT keyword containment", "NOT tags:golang", 3},
		{"NOT keyword wildcard", "NOT tags:*go*", 3},
		{"MustNot prefix containment", "-tags:golang", 3},
		{"NOT keyword in a compound", "title:hello AND NOT tags:rust", 1},
		{"NOT keyword excludes its own row", "title:hello AND NOT tags:golang", 0},
		{"NOT keyword on a scalar", "NOT title:hello", 3},
		{"NOT keyword on null, array field", "NOT tags:null", 3},
		{"NOT keyword on null, scalar field", "NOT title:null", 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countMatching(t, db, tt.filter); got != tt.want {
				t.Errorf("filter %q matched %d rows, want %d", tt.filter, got, tt.want)
			}
		})
	}
}

// Regression guard for the cross-element-boundary false positive: casting the
// array to text let a pattern span the separator between two elements.
//
// This cannot be driven through NewParser+ParseToSQL like the other cases:
// go-lucene's lexer treats an unquoted comma as a hard token separator (on par
// with whitespace — see internal/lex/lex.go's lexSpace), so the filter string
// `tags:*ha,be*` never reaches the SQL driver as one term. It parses as two
// ANDed terms (`tags:*ha` AND a fieldless `be*`), which fails at ParseToSQL
// with "nil value in expression" — a parser-level artifact, not a defect in
// this package's rendering. Escaping the comma doesn't help either: the lexer
// consumes the backslash but leaves it in the token value unstripped, so the
// resulting LIKE pattern would carry a literal backslash that never appears
// in the real JSON text, making the assertion pass for the wrong reason.
//
// So this test builds the expr tree directly (bypassing the string parser)
// and drives it through the same NewSQLDriver + RenderParam path that
// ParseToSQL uses internally, then executes the real generated SQL exactly
// like every other case in this file.
func TestExecuted_WildcardDoesNotSpanElements(t *testing.T) {
	db := newExecDB(t)

	if _, err := db.Exec(`INSERT INTO articles VALUES ('5','pair','["alpha","beta"]')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	p, err := NewParser(Article{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	driver, err := NewSQLDriver(p.Fields, "sqlite")
	if err != nil {
		t.Fatalf("NewSQLDriver: %v", err)
	}

	where, params, err := driver.RenderParam(expr.LIKE("tags", expr.WILD("*ha,be*")))
	if err != nil {
		t.Fatalf("RenderParam: %v", err)
	}

	var n int
	query := "SELECT count(*) FROM articles WHERE " + where
	if err := db.QueryRow(query, params...).Scan(&n); err != nil {
		t.Fatalf("executing %q (params %v): %v", query, params, err)
	}
	if n != 0 {
		t.Errorf("wildcard must not match across element boundaries, matched %d rows", n)
	}
}

// Regression guard for what issue #224 actually reported: ParseToSQL returned
// a nil error for a query it could not render, so the failure surfaced as a
// 500 from the database driver instead of a 400 from the parser.
//
// Row counts for these filters are asserted in TestExecuted_ArrayContainment;
// what this test pins is the contract that the parse step reports success only
// when the SQL it produced is actually executable — checked by executing it.
func TestExecuted_Issue224Repro(t *testing.T) {
	db := newExecDB(t)

	p, err := NewParser(Article{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	for _, filter := range []string{"title:hello", "tags:golang", "tags:*go*", "NOT tags:golang"} {
		t.Run(filter, func(t *testing.T) {
			where, params, err := p.ParseToSQL(filter, "sqlite")
			if err != nil {
				t.Fatalf("ParseToSQL(%q) returned an error: %v", filter, err)
			}

			var n int
			query := "SELECT count(*) FROM articles WHERE " + where
			if err := db.QueryRow(query, params...).Scan(&n); err != nil {
				t.Fatalf("ParseToSQL(%q) reported success but %q failed to execute: %v", filter, query, err)
			}
		})
	}
}
