package lucene

import (
	"database/sql"
	"testing"
)

// Note models a table whose text columns may hold NULL.
type Note struct {
	Id     string `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Status string `json:"status"`
}

// newNotesDB creates an in-memory SQLite database with a known fixture.
//
//	id | title   | body | status
//	 1 | room 12 | x    | open
//	 2 | hall    | NULL | NULL
//	 3 | room 7  | NULL | closed
//	 4 | yard    | y    | open
func newNotesDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE notes (id TEXT, title TEXT, body TEXT, status TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO notes VALUES
		('1','room 12','x','open'),
		('2','hall',NULL,NULL),
		('3','room 7',NULL,'closed'),
		('4','yard','y','open')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return db
}

func countNotes(t *testing.T, db *sql.DB, filter string) int {
	t.Helper()

	p, err := NewParser(Note{})
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}
	where, params, err := p.ParseToSQL(filter, "sqlite")
	if err != nil {
		t.Fatalf("ParseToSQL(%q): %v", filter, err)
	}
	var n int
	query := "SELECT count(*) FROM notes WHERE " + where
	if err := db.QueryRow(query, params...).Scan(&n); err != nil {
		t.Fatalf("executing %q (from filter %q): %v", query, filter, err)
	}
	return n
}

// A negation keeps the rows its operand does not match, including a row where
// the operand reads a NULL column. Plain NOT turns NULL into NULL, so such a
// row was dropped: a negated word searches every text column, and one NULL
// column in a row was enough to drop it.
func TestExecuted_NegationKeepsRowsWithNullColumns(t *testing.T) {
	db := newNotesDB(t)

	tests := []struct {
		name   string
		filter string
		want   int
	}{
		{"negated word with -", "-room", 2},
		{"negated word with NOT", "NOT room", 2},
		{"negated word with !", "!room", 2},
		{"word and negated word", "hall -room", 1},
		{"negated field term over a NULL column", "-status:closed", 3},
		{"NOT field term over a NULL column", "NOT status:closed", 3},
		{"negated group over NULL columns", "-(status:closed OR body:x)", 2},
		{"negated wildcard over a NULL column", "-body:*x*", 3},
		{"negated existence matches the NULL rows", "-body:*", 2},

		// Unchanged: the positive forms and the IS NOT NULL shortcut.
		{"word", "room", 2},
		{"field term", "status:open", 2},
		{"NOT field:null stays IS NOT NULL", "NOT status:null", 3},
		{"field:null", "status:null", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countNotes(t, db, tt.filter); got != tt.want {
				t.Errorf("%q matched %d rows, want %d", tt.filter, got, tt.want)
			}
		})
	}
}
