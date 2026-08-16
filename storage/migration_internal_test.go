package storage

import (
	"errors"
	"slices"
	"testing"
)

// recordingAdapter is a minimal StorageAdapter that records the
// sequence of Execute calls and optionally returns an error
// starting at a given call index. It is used to exercise
// rollbackMigration, which only needs Execute to be real.
type recordingAdapter struct {
	StorageAdapter

	calls       []string
	failAtIndex int // 0-based; negative = never fail
	failErr     error
}

func (r *recordingAdapter) Execute(stmt string) error {
	r.calls = append(r.calls, stmt)
	if r.failAtIndex >= 0 && len(r.calls)-1 == r.failAtIndex {
		return r.failErr
	}
	return nil
}

func TestRollbackMigrationExecutesInReverseOrder(t *testing.T) {
	rec := &recordingAdapter{failAtIndex: -1}
	m := &DatabaseMigration{storage: rec}

	mig := MigrationFile{
		Migrations: []Migration{
			{Migrate: "UP-1", Rollback: "DOWN-1"},
			{Migrate: "UP-2", Rollback: "DOWN-2"},
			{Migrate: "UP-3", Rollback: "DOWN-3"},
		},
	}

	if err := m.rollbackMigration(mig); err != nil {
		t.Fatalf("rollbackMigration: %v", err)
	}

	want := []string{"DOWN-3", "DOWN-2", "DOWN-1"}
	if len(rec.calls) != len(want) {
		t.Fatalf("Execute call count = %d; want %d", len(rec.calls), len(want))
	}
	for i, got := range rec.calls {
		if got != want[i] {
			t.Fatalf("call[%d] = %q; want %q", i, got, want[i])
		}
	}
}

func TestRollbackMigrationStopsAtFirstFailure(t *testing.T) {
	boom := errors.New("rollback boom")
	rec := &recordingAdapter{failAtIndex: 0, failErr: boom}
	m := &DatabaseMigration{storage: rec}

	mig := MigrationFile{
		Migrations: []Migration{
			{Migrate: "UP-1", Rollback: "DOWN-1"},
			{Migrate: "UP-2", Rollback: "DOWN-2"},
		},
	}

	err := m.rollbackMigration(mig)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
	// Rollback runs statements in reverse. The first reversed
	// call (DOWN-2) fails so DOWN-1 must never run.
	if len(rec.calls) != 1 || rec.calls[0] != "DOWN-2" {
		t.Fatalf("expected single DOWN-2 call before error, got %v", rec.calls)
	}
}

func TestSortMigrationKeysOrdersNumericallyNotLexically(t *testing.T) {
	// The unsorted set deliberately mixes one-, two-, and three-digit ids,
	// and includes two keys that share id 100 to exercise the tie-break.
	// A lexical sort would place "100__" before "10__".."99__"; the numeric
	// sort must not.
	keys := []string{
		"100__add_widgets", "2__add_users", "10__add_orders",
		"9__add_carts", "100__add_audit", "1__init", "99__add_index",
	}
	sortMigrationKeys(keys)

	want := []string{
		"1__init", "2__add_users", "9__add_carts", "10__add_orders",
		"99__add_index", "100__add_audit", "100__add_widgets",
	}
	if len(keys) != len(want) {
		t.Fatalf("len = %d; want %d", len(keys), len(want))
	}
	for i, got := range keys {
		if got != want[i] {
			t.Fatalf("keys[%d] = %q; want %q (full order: %v)", i, got, want[i], keys)
		}
	}
}

func TestSortMigrationKeysFallsBackToLexicalForUnparseablePrefix(t *testing.T) {
	// Keys whose prefix is not a number must still sort deterministically
	// rather than panic; runMigrations reports the bad id when it gets there.
	keys := []string{"10__ok", "bad__name", "2__ok", "another"}
	sortMigrationKeys(keys)

	// Numeric-prefixed keys keep numeric order among themselves; the rest are
	// ordered lexically relative to whatever they compare against.
	if got := slices.Index(keys, "2__ok"); got > slices.Index(keys, "10__ok") {
		t.Fatalf("2__ok should precede 10__ok, got order %v", keys)
	}
}

func TestMigrationIDRequiresDoubleUnderscoreSeparator(t *testing.T) {
	// A single underscore is a snake_case word boundary inside the
	// description, not an id boundary, so a name lacking "__" is malformed
	// and must error loudly rather than misparse its prefix as an id.
	if _, err := migrationID("1_init.yaml"); err == nil {
		t.Fatal("expected error for filename without __ separator")
	}
	if got, err := migrationID("12__add_users.yaml"); err != nil || got != 12 {
		t.Fatalf("migrationID = %d, %v; want 12, nil", got, err)
	}
}

func TestGetMigrationFilesReturnsEmptyForUninitializedConfigFs(t *testing.T) {
	// ConfigFs is a zero-value embed.FS unless the embedding
	// application has wired its own in, so ReadDir fails
	// silently and we return an empty map. This is the
	// documented behaviour and underpins the sqlite-happy-path
	// Migrate test in migration_test.go, so it's worth pinning.
	m := &DatabaseMigration{storageProvider: SQLITE}

	got, err := m.getMigrationFiles()
	if err != nil {
		t.Fatalf("getMigrationFiles: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty migration map, got %v", got)
	}
}
