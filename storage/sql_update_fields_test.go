package storage_test

import (
	"strings"
	"testing"

	"github.com/tink3rlabs/magic/storage"
)

// fieldItem has two independent non-key columns so a test can change one and
// assert the other survived. scopedItem cannot serve here: its only spare
// column doubles as the filter column.
type fieldItem struct {
	Id     string `json:"id" gorm:"primaryKey;column:id"`
	Tenant string `json:"tenant" gorm:"column:tenant"`
	Name   string `json:"name" gorm:"column:name"`
	Color  string `json:"color" gorm:"column:color"`
}

func (fieldItem) TableName() string { return "sql_field_items" }

func setupFieldItems(t *testing.T) *storage.SQLAdapter {
	t.Helper()
	m := storage.GetMemoryAdapterInstance()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS sql_field_items (id TEXT PRIMARY KEY, tenant TEXT, name TEXT, color TEXT)`,
		`DELETE FROM sql_field_items`,
		`INSERT INTO sql_field_items VALUES ('c1', 'A', 'original', 'red')`,
	} {
		if err := m.Execute(stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}
	return m.DB
}

func fieldItemByID(t *testing.T, sql *storage.SQLAdapter, id string) fieldItem {
	t.Helper()
	var got fieldItem
	if err := sql.Get(&got, map[string]any{"id": id}); err != nil {
		t.Fatalf("Get %s: %v", id, err)
	}
	return got
}

// TestSQLAdapterUpdateWritesOnlyNamedFields is the reason WithFields exists:
// two callers that read the same row and then change different fields must both
// survive. Without it each Update writes every field, so the second caller
// writes back the value it read for the field it never touched and silently
// reverts the first caller's change.
func TestSQLAdapterUpdateWritesOnlyNamedFields(t *testing.T) {
	sql := setupFieldItems(t)
	id := map[string]any{"id": "c1"}

	// Both callers read before either writes.
	var a, b fieldItem
	if err := sql.Get(&a, id); err != nil {
		t.Fatalf("read a: %v", err)
	}
	if err := sql.Get(&b, id); err != nil {
		t.Fatalf("read b: %v", err)
	}

	a.Name = "renamed"
	if err := sql.Update(&a, id, storage.WithFields("name")); err != nil {
		t.Fatalf("update a: %v", err)
	}
	b.Color = "blue"
	if err := sql.Update(&b, id, storage.WithFields("color")); err != nil {
		t.Fatalf("update b: %v", err)
	}

	if got := fieldItemByID(t, sql, "c1"); got.Name != "renamed" || got.Color != "blue" {
		t.Fatalf("lost update: got name=%q color=%q; want name=%q color=%q",
			got.Name, got.Color, "renamed", "blue")
	}
}

// TestSQLAdapterUpdateNamedFieldWritesZeroValue pins the semantics a naive
// implementation gets wrong: Updates with a struct skips zero-valued fields
// unless they are selected, so clearing a field has to go through Select.
func TestSQLAdapterUpdateNamedFieldWritesZeroValue(t *testing.T) {
	sql := setupFieldItems(t)

	item := fieldItem{Id: "c1", Tenant: "A", Name: ""}
	if err := sql.Update(&item, map[string]any{"id": "c1"}, storage.WithFields("name")); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got := fieldItemByID(t, sql, "c1")
	if got.Name != "" {
		t.Fatalf("name = %q; want it cleared", got.Name)
	}
	if got.Color != "red" {
		t.Fatalf("color = %q; want %q untouched", got.Color, "red")
	}
}

// TestSQLAdapterUpdateWithoutFieldsWritesEveryField keeps the default: an Update
// with no WithFields still writes the whole row.
func TestSQLAdapterUpdateWithoutFieldsWritesEveryField(t *testing.T) {
	sql := setupFieldItems(t)

	item := fieldItem{Id: "c1", Tenant: "A", Name: "renamed", Color: "green"}
	if err := sql.Update(&item, map[string]any{"id": "c1"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got := fieldItemByID(t, sql, "c1"); got.Name != "renamed" || got.Color != "green" {
		t.Fatalf("got name=%q color=%q; want both written", got.Name, got.Color)
	}
}

// TestSQLAdapterUpdateNamedFieldsRespectFilter pins that WithFields narrows what
// is written and never widens what is matched.
func TestSQLAdapterUpdateNamedFieldsRespectFilter(t *testing.T) {
	sql := setupFieldItems(t)

	item := fieldItem{Id: "c1", Tenant: "B", Name: "overwritten"}
	err := sql.Update(&item, map[string]any{"tenant": "B"}, storage.WithFields("name"))
	if err == nil {
		t.Fatalf("Update = nil; want ErrNotFound for a row outside the filter")
	}
	if got := fieldItemByID(t, sql, "c1").Name; got != "original" {
		t.Fatalf("name = %q; want %q unchanged", got, "original")
	}
}

func TestSQLAdapterUpdateRejectsUnknownField(t *testing.T) {
	sql := setupFieldItems(t)

	item := fieldItem{Id: "c1", Tenant: "A", Name: "renamed"}
	err := sql.Update(&item, map[string]any{"id": "c1"}, storage.WithFields("name", "nmae"))
	if err == nil {
		t.Fatalf("Update = nil; want an error naming the unknown field")
	}
	if !strings.Contains(err.Error(), "nmae") {
		t.Fatalf("error %q; want it to name the unknown field", err)
	}
	if got := fieldItemByID(t, sql, "c1").Name; got != "original" {
		t.Fatalf("name = %q; want the write rejected before it ran", got)
	}
}

// TestSQLAdapterUpdateRejectsEmptyFieldList covers the case a caller reaches by
// spreading a computed slice that turned out empty -- WithFields(changed...)
// where nothing changed. Writing every field would be the worst possible
// reading of that, so it is an error; skipping the call is the caller's
// decision to make.
func TestSQLAdapterUpdateRejectsEmptyFieldList(t *testing.T) {
	sql := setupFieldItems(t)

	item := fieldItem{Id: "c1", Tenant: "A", Name: "renamed"}
	err := sql.Update(&item, map[string]any{"id": "c1"}, storage.WithFields())
	if err == nil {
		t.Fatalf("Update = nil; want an error for an empty field list")
	}
	if got := fieldItemByID(t, sql, "c1").Name; got != "original" {
		t.Fatalf("name = %q; want nothing written", got)
	}
}

// TestSQLAdapterUpdateRejectsUnsafeFieldName covers the injection path: field
// names reach SELECT, which is not parameterized.
func TestSQLAdapterUpdateRejectsUnsafeFieldName(t *testing.T) {
	sql := setupFieldItems(t)

	item := fieldItem{Id: "c1", Tenant: "A", Name: "renamed"}
	err := sql.Update(&item, map[string]any{"id": "c1"}, storage.WithFields("name, color"))
	if err == nil {
		t.Fatalf("Update = nil; want an error for an unsafe field name")
	}
	if got := fieldItemByID(t, sql, "c1"); got.Name != "original" || got.Color != "red" {
		t.Fatalf("got name=%q color=%q; want nothing written", got.Name, got.Color)
	}
}

// A non-[]string field list needed a test of its own while this was a map key.
// WithFields is typed, so the compiler rejects that now and there is nothing
// left to assert.
