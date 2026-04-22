package storage_test

import (
	"testing"

	"github.com/tink3rlabs/magic/storage"
)

// stubAdapter is a minimal StorageAdapter used to drive
// DatabaseMigration paths that short-circuit before any I/O.
// Methods the exercised path does NOT call fall through to the
// embedded nil StorageAdapter, so invoking them accidentally
// surfaces as a panic (test failure) rather than silent success.
type stubAdapter struct {
	storage.StorageAdapter
	typ      storage.StorageAdapterType
	provider storage.StorageProviders
}

func (s *stubAdapter) GetType() storage.StorageAdapterType   { return s.typ }
func (s *stubAdapter) GetProvider() storage.StorageProviders { return s.provider }

func TestNewDatabaseMigrationCapturesStorageMetadata(t *testing.T) {
	adapter := &stubAdapter{typ: storage.MEMORY, provider: storage.SQLITE}

	// The constructor must only read metadata via GetType /
	// GetProvider; touching any other method would panic on
	// the nil embedded adapter.
	m := storage.NewDatabaseMigration(adapter)
	if m == nil {
		t.Fatalf("NewDatabaseMigration returned nil")
	}
}

func TestMigrateShortCircuitsForDynamoDB(t *testing.T) {
	// Any Execute/CreateSchema/... call would panic because the
	// embedded StorageAdapter is nil. Reaching the end of this
	// test without panic proves Migrate took the DYNAMODB
	// short-circuit branch and only logged.
	adapter := &stubAdapter{typ: storage.DYNAMODB}
	m := storage.NewDatabaseMigration(adapter)

	m.Migrate()
}

func TestMigrateHappyPathOnSQLiteWithNoConfiguredMigrations(t *testing.T) {
	// The memory adapter's underlying sqlite is always
	// available in this test process. ConfigFs is the
	// package-level zero-value embed.FS, so getMigrationFiles
	// returns an empty map, which drives runMigrations through
	// its "no work to do" branch without any logger.Fatal.
	adapter := storage.GetMemoryAdapterInstance()
	m := storage.NewDatabaseMigration(adapter)

	m.Migrate()

	// Side-effect: the migrations table must now exist.
	// Querying it confirms that CreateSchema and
	// CreateMigrationTable both ran cleanly.
	if _, err := adapter.GetLatestMigration(); err != nil {
		t.Fatalf("GetLatestMigration after Migrate: %v", err)
	}
}
