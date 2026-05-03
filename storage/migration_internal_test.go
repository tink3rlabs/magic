package storage

import (
	"errors"
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
