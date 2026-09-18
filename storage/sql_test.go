package storage_test

import (
	"errors"
	"testing"

	magicerrors "github.com/tink3rlabs/magic/errors"
	"github.com/tink3rlabs/magic/storage"
)

// This file exercises the raw SQL/memory adapter instances
// (not the telemetry wrapper) to cover the large block of
// code that the wrapper-based tests in telemetry_test.go skip
// over: the non-context delegates and the lucene-backed search
// path, including pagination cursor round-trips through
// findFieldByJSONTag and executePaginatedQuery.
//
// We use the existing in-memory sqlite singleton to keep the
// test suite hermetic.

type sqlCoverageItem struct {
	Id   string `json:"id" gorm:"primaryKey;column:id"`
	Name string `json:"name" gorm:"column:name"`
}

func (sqlCoverageItem) TableName() string { return "sql_coverage_items" }

// setupSQLCoverage creates (once per run) and truncates the
// per-test table against the in-process sqlite memory adapter.
// Both the raw MemoryAdapter and its embedded SQLAdapter are
// returned so tests can call either the memory-level or
// sql-level non-context methods.
func setupSQLCoverage(t *testing.T) (*storage.MemoryAdapter, *storage.SQLAdapter) {
	t.Helper()
	m := storage.GetMemoryAdapterInstance()
	if err := m.Execute(`CREATE TABLE IF NOT EXISTS sql_coverage_items (id TEXT PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if err := m.Execute(`DELETE FROM sql_coverage_items`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return m, m.DB
}

type scopedItem struct {
	Id        string  `json:"id" gorm:"primaryKey;column:id"`
	Tenant    string  `json:"tenant" gorm:"column:tenant"`
	Name      string  `json:"name" gorm:"column:name"`
	DeletedAt *string `json:"deleted_at" gorm:"column:deleted_at"`
}

func (scopedItem) TableName() string { return "sql_scoped_items" }

// setupScopedItems seeds two live tenant-A rows, one tenant-B row and one
// soft-deleted tenant-A row.
func setupScopedItems(t *testing.T) *storage.SQLAdapter {
	t.Helper()
	m := storage.GetMemoryAdapterInstance()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS sql_scoped_items (id TEXT PRIMARY KEY, tenant TEXT, name TEXT, deleted_at TEXT)`,
		`DELETE FROM sql_scoped_items`,
		`INSERT INTO sql_scoped_items VALUES ('a1', 'A', 'one', NULL), ('a2', 'A', 'two', NULL), ('b1', 'B', 'other', NULL), ('a3', 'A', 'gone', '2020-01-01')`,
	} {
		if err := m.Execute(stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}
	return m.DB
}

func scopedItemByID(t *testing.T, sql *storage.SQLAdapter, id string) scopedItem {
	t.Helper()
	var got scopedItem
	if err := sql.Get(&got, map[string]any{"id": id}); err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	return got
}

func TestSQLAdapterUpdateOnlyWritesTheItemRow(t *testing.T) {
	sql := setupScopedItems(t)

	if err := sql.Update(&scopedItem{Id: "a1", Tenant: "A", Name: "renamed"}, map[string]any{"tenant": "A"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := scopedItemByID(t, sql, "a1").Name; got != "renamed" {
		t.Fatalf("a1 name = %q; want %q", got, "renamed")
	}
	if got := scopedItemByID(t, sql, "a2").Name; got != "two" {
		t.Fatalf("a2 name = %q; want %q", got, "two")
	}
}

func TestSQLAdapterUpdateWithUnchangedValuesSucceeds(t *testing.T) {
	sql := setupScopedItems(t)

	if err := sql.Update(&scopedItem{Id: "a1", Tenant: "A", Name: "one"}, map[string]any{"tenant": "A"}); err != nil {
		t.Fatalf("Update with unchanged values: %v", err)
	}
}

func TestSQLAdapterUpdateRejectsRowOutsideFilter(t *testing.T) {
	sql := setupScopedItems(t)

	err := sql.Update(&scopedItem{Id: "b1", Tenant: "A", Name: "overwritten"}, map[string]any{"tenant": "A"})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Update = %v; want ErrNotFound", err)
	}
	if got := scopedItemByID(t, sql, "b1"); got.Tenant != "B" || got.Name != "other" {
		t.Fatalf("b1 = %+v; want it unchanged", got)
	}
}

func TestSQLAdapterUpdateDoesNotCreateMissingRow(t *testing.T) {
	sql := setupScopedItems(t)

	err := sql.Update(&scopedItem{Id: "missing", Tenant: "A", Name: "new"}, map[string]any{"tenant": "A"})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Update = %v; want ErrNotFound", err)
	}
	var got scopedItem
	if err := sql.Get(&got, map[string]any{"id": "missing"}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get missing = %v (%+v); want ErrNotFound", err, got)
	}
}

func TestSQLAdapterUpdateRequiresPrimaryKey(t *testing.T) {
	sql := setupScopedItems(t)

	if err := sql.Update(&scopedItem{Tenant: "A", Name: "everyone"}, map[string]any{"tenant": "A"}); err == nil {
		t.Fatal("Update without a primary key = nil; want an error")
	}
	for _, id := range []string{"a1", "a2"} {
		if got := scopedItemByID(t, sql, id).Name; got == "everyone" {
			t.Fatalf("%s was overwritten by an update without a primary key", id)
		}
	}
	total, err := sql.Count(&[]scopedItem{}, map[string]any{"id": ""})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 0 {
		t.Fatalf("rows with empty id = %d; want 0", total)
	}
}

func TestSQLAdapterUpdateRejectsNonStructItems(t *testing.T) {
	sql := setupScopedItems(t)

	for name, item := range map[string]any{
		"slice":       &[]scopedItem{{Id: "a1", Tenant: "A", Name: "bulk"}},
		"nil pointer": (*scopedItem)(nil),
	} {
		if err := sql.Update(item, map[string]any{"tenant": "A"}); err == nil {
			t.Fatalf("%s: Update = nil; want an error", name)
		}
	}
	if got := scopedItemByID(t, sql, "a1").Name; got != "one" {
		t.Fatalf("a1 name = %q; want %q", got, "one")
	}
}

func TestSQLAdapterUpdateSkipsSoftDeletedRow(t *testing.T) {
	sql := setupScopedItems(t)

	err := sql.Update(&scopedItem{Id: "a3", Tenant: "A", Name: "restored"}, map[string]any{"tenant": "A", "deleted_at": nil})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Update = %v; want ErrNotFound", err)
	}
	if got := scopedItemByID(t, sql, "a3"); got.DeletedAt == nil || got.Name != "gone" {
		t.Fatalf("a3 = %+v; want it still soft-deleted and unchanged", got)
	}
}

func TestSQLAdapterNonContextCRUDRoundTrip(t *testing.T) {
	_, sql := setupSQLCoverage(t)

	if err := sql.Create(&sqlCoverageItem{Id: "1", Name: "alpha"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var got sqlCoverageItem
	if err := sql.Get(&got, map[string]any{"id": "1"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "alpha" {
		t.Fatalf("Name = %q; want %q", got.Name, "alpha")
	}

	if err := sql.Update(&sqlCoverageItem{Id: "1", Name: "beta"}, map[string]any{"id": "1"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got = sqlCoverageItem{}
	if err := sql.Get(&got, map[string]any{"id": "1"}); err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Name != "beta" {
		t.Fatalf("Name after update = %q; want %q", got.Name, "beta")
	}

	total, err := sql.Count(&[]sqlCoverageItem{}, map[string]any{"id": "1"})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 1 {
		t.Fatalf("Count = %d; want 1", total)
	}

	if err := sql.Delete(&sqlCoverageItem{}, map[string]any{"id": "1"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got = sqlCoverageItem{}
	if err := sql.Get(&got, map[string]any{"id": "1"}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get after delete = %v; want ErrNotFound", err)
	}
}

func TestSQLAdapterPingAndSchemaPassThroughs(t *testing.T) {
	_, sql := setupSQLCoverage(t)

	if err := sql.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	// Memory adapter is configured without a schema; the raw
	// SQLAdapter should report the same empty value.
	if got := sql.GetSchemaName(); got != "" {
		t.Fatalf("GetSchemaName = %q; want empty", got)
	}
}

func TestSQLAdapterGetLatestMigrationReturnsMaxId(t *testing.T) {
	_, sql := setupSQLCoverage(t)

	// Make sure the migrations table exists and starts empty.
	if err := sql.CreateMigrationTable(); err != nil {
		t.Fatalf("CreateMigrationTable: %v", err)
	}
	if err := sql.Execute(`DELETE FROM migrations`); err != nil {
		t.Fatalf("truncate migrations: %v", err)
	}

	// Insert a couple of migrations and make sure the max id
	// is returned.
	if err := sql.UpdateMigrationTable(10, "bootstrap", "bootstrap"); err != nil {
		t.Fatalf("UpdateMigrationTable 10: %v", err)
	}
	if err := sql.UpdateMigrationTable(20, "seed", "seed"); err != nil {
		t.Fatalf("UpdateMigrationTable 20: %v", err)
	}

	latest, err := sql.GetLatestMigration()
	if err != nil {
		t.Fatalf("GetLatestMigration: %v", err)
	}
	if latest != 20 {
		t.Fatalf("GetLatestMigration = %d; want 20", latest)
	}
}

func TestSQLAdapterQueryReturnsNotImplemented(t *testing.T) {
	_, sql := setupSQLCoverage(t)
	if _, err := sql.Query(&[]sqlCoverageItem{}, "SELECT 1", 10, ""); err == nil {
		t.Fatalf("expected Query to return not-implemented error")
	}
}

func TestSQLAdapterListPaginatesWithCursorRoundTrip(t *testing.T) {
	_, sql := setupSQLCoverage(t)

	for _, id := range []string{"a", "b", "c"} {
		if err := sql.Create(&sqlCoverageItem{Id: id, Name: id}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	var page1 []sqlCoverageItem
	cursor, err := sql.List(&page1, "id", nil, 2, "")
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d; want 2", len(page1))
	}
	if cursor == "" {
		t.Fatalf("expected a non-empty cursor after page 1")
	}
	if page1[0].Id != "a" || page1[1].Id != "b" {
		t.Fatalf("page1 = %+v; want [a b]", page1)
	}

	var page2 []sqlCoverageItem
	nextCursor, err := sql.List(&page2, "id", nil, 2, cursor)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if nextCursor != "" {
		t.Fatalf("expected empty cursor on final page, got %q", nextCursor)
	}
	if len(page2) != 1 || page2[0].Id != "c" {
		t.Fatalf("page2 = %+v; want [c]", page2)
	}
}

func TestSQLAdapterListDescendingSort(t *testing.T) {
	_, sql := setupSQLCoverage(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := sql.Create(&sqlCoverageItem{Id: id, Name: id}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	var page []sqlCoverageItem
	_, err := sql.List(&page, "id", nil, 10, "", storage.WithSortDirection(storage.Descending))
	if err != nil {
		t.Fatalf("List DESC: %v", err)
	}
	if len(page) != 3 || page[0].Id != "c" || page[2].Id != "a" {
		t.Fatalf("DESC page = %+v; want c→a", page)
	}
}

func TestSQLAdapterListRejectsInvalidCursor(t *testing.T) {
	_, sql := setupSQLCoverage(t)
	var page []sqlCoverageItem
	// A non-base64 cursor must surface as an error, not a panic
	// or silent reset to page 1.
	_, err := sql.List(&page, "id", nil, 10, "!!!not-base64!!!")
	if err == nil {
		t.Fatalf("expected error for invalid base64 cursor")
	}
}

func TestSQLAdapterListRejectsInvalidSortKey(t *testing.T) {
	_, sql := setupSQLCoverage(t)
	var page []sqlCoverageItem
	_, err := sql.List(&page, "bad sort;DROP", nil, 10, "")
	if err == nil {
		t.Fatalf("expected error for invalid sort key")
	}
}

func TestSQLAdapterListRejectsInvalidSortDirection(t *testing.T) {
	_, sql := setupSQLCoverage(t)
	var page []sqlCoverageItem
	_, err := sql.List(&page, "id", nil, 10, "", storage.WithSortDirection(storage.SortingDirection("sideways")))
	if err == nil {
		t.Fatalf("expected error for invalid sort direction")
	}
}

func TestSQLAdapterSearchEmptyQueryFallsBackToList(t *testing.T) {
	_, sql := setupSQLCoverage(t)
	for _, id := range []string{"a", "b"} {
		if err := sql.Create(&sqlCoverageItem{Id: id, Name: id}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}

	var page []sqlCoverageItem
	if _, err := sql.Search(&page, "id", "", 10, ""); err != nil {
		t.Fatalf("Search empty query: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("Search empty query len = %d; want 2", len(page))
	}
}

func TestSQLAdapterSearchWithLuceneQueryMatchesField(t *testing.T) {
	_, sql := setupSQLCoverage(t)
	if err := sql.Create(&sqlCoverageItem{Id: "1", Name: "alpha"}); err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	if err := sql.Create(&sqlCoverageItem{Id: "2", Name: "beta"}); err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	var page []sqlCoverageItem
	if _, err := sql.Search(&page, "id", `name:alpha`, 10, ""); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(page) != 1 || page[0].Id != "1" {
		t.Fatalf("Search result = %+v; want [1/alpha]", page)
	}
}

func TestSQLAdapterSearchInvalidFieldIsBadRequest(t *testing.T) {
	_, sql := setupSQLCoverage(t)
	var page []sqlCoverageItem
	_, err := sql.Search(&page, "id", `nonexistent:x`, 10, "")
	if err == nil {
		t.Fatalf("expected error when searching against a non-existent field")
	}
	var br *magicerrors.BadRequest
	if !errors.As(err, &br) {
		t.Fatalf("err = %T (%v); want *errors.BadRequest", err, err)
	}
}

func TestSQLAdapterSearchRejectsInvalidSortDirection(t *testing.T) {
	_, sql := setupSQLCoverage(t)
	var page []sqlCoverageItem
	_, err := sql.Search(&page, "id", "", 10, "", storage.WithSortDirection(storage.SortingDirection("weird")))
	if err == nil {
		t.Fatalf("expected error from Search for invalid sort direction")
	}
}

func TestMemoryAdapterDelegatesNonContextCountAndQuery(t *testing.T) {
	m, _ := setupSQLCoverage(t)

	if err := m.Create(&sqlCoverageItem{Id: "only", Name: "x"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	total, err := m.Count(&[]sqlCoverageItem{}, map[string]any{"id": "only"})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != 1 {
		t.Fatalf("Count = %d; want 1", total)
	}

	// MemoryAdapter.Query delegates straight to SQLAdapter.Query
	// which currently returns a not-implemented error. Covering
	// the delegate pins that contract.
	if _, err := m.Query(&[]sqlCoverageItem{}, "SELECT 1", 10, ""); err == nil {
		t.Fatalf("expected memory.Query to propagate not-implemented error")
	}
}
