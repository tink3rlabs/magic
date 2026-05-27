package storage_test

import (
	"testing"

	"github.com/tink3rlabs/magic/storage"
)

// sortDirRow uses a dedicated table so it can run against the shared
// in-memory adapter singleton without colliding with other tests.
type sortDirRow struct {
	ID string `json:"id"`
}

func (sortDirRow) TableName() string { return "memtest_sortdir" }

// The in-memory adapter delegates to an embedded SQLAdapter. It must forward
// the variadic params (which carry SortDirectionKey) so that ordering and
// validation behave the same as the SQL adapter — see storage/memory.go.
func TestMemoryAdapterForwardsSortDirection(t *testing.T) {
	adapter, err := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, nil)
	if err != nil {
		t.Fatalf("GetInstance(MEMORY): %v", err)
	}
	if err := adapter.Execute(`CREATE TABLE IF NOT EXISTS memtest_sortdir (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Execute(`DROP TABLE IF EXISTS memtest_sortdir`) })

	for _, id := range []string{"01", "02", "03"} {
		if err := adapter.Create(sortDirRow{ID: id}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	var desc []sortDirRow
	if _, err := adapter.List(&desc, "id", nil, 10, "", map[string]any{storage.SortDirectionKey: "DESC"}); err != nil {
		t.Fatalf("list DESC: %v", err)
	}
	got := []string{}
	for _, r := range desc {
		got = append(got, r.ID)
	}
	want := []string{"03", "02", "01"}
	if len(got) != len(want) {
		t.Fatalf("DESC list length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DESC ordering = %v, want %v (SortDirectionKey was dropped?)", got, want)
		}
	}

	// An invalid direction must surface an error, not be silently ignored.
	var bad []sortDirRow
	if _, err := adapter.List(&bad, "id", nil, 10, "", map[string]any{storage.SortDirectionKey: "sideways"}); err == nil {
		t.Fatalf("invalid sort direction returned no error; want error")
	}
}
