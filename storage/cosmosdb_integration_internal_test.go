package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
)

// These tests run against a real CosmosDB API, set with
// COSMOSDB_TEST_ENDPOINT, and skip without it. The Linux emulator serves it:
//
//	docker run -p 8081:8081 mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator:vnext-preview --protocol http
//	COSMOSDB_TEST_ENDPOINT=http://localhost:8081 go test ./storage/ -run CosmosDB
//
// Each test gets its own database, since the emulator fails to delete one
// that was just used. Containers are named from the item type, as the adapter
// names them.

// cosmosEmulatorKey is the fixed, publicly documented key of the CosmosDB
// emulator. It opens nothing else.
const cosmosEmulatorKey = "C2y6yDjf5/R+ob0N8A7Cgv30VRDJIWEHLM+4QDU5DE2nQ9nDuVTqobD4b8mGGyPMbIZnqyMsEcaGQy67XIw/Jw=="

// cosmosItem is partitioned on /pk, the adapter's default partition key field.
type cosmosItem struct {
	Id    string `json:"id"`
	Pk    string `json:"pk,omitempty"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Note  string `json:"note,omitempty"`
}

// cosmosTenantItem is partitioned on /tenant.
type cosmosTenantItem struct {
	Id     string `json:"id"`
	Tenant string `json:"tenant"`
	Name   string `json:"name"`
}

func newCosmosTestAdapter(t *testing.T) *CosmosDBAdapter {
	t.Helper()
	endpoint := os.Getenv("COSMOSDB_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("COSMOSDB_TEST_ENDPOINT is not set; skipping CosmosDB integration tests")
	}
	database := fmt.Sprintf("magic_test_%d", time.Now().UnixNano())
	config := map[string]string{"endpoint": endpoint, "key": cosmosEmulatorKey, "database": database}
	// Deliberately not GetCosmosDBAdapterInstance: that caches a process-wide
	// singleton, so the first config to reach it would decide every later test.
	s := &CosmosDBAdapter{config: config, sessionTokens: newCosmosSessionTokenStore(config)}
	s.OpenConnection()

	ctx := context.Background()
	if _, err := s.client.CreateDatabase(ctx, azcosmos.DatabaseProperties{ID: database}, nil); err != nil {
		t.Fatalf("CosmosDB at %s is not reachable: %v", endpoint, err)
	}
	for container, path := range map[string]string{
		s.getContainerName(&cosmosItem{}):       "/pk",
		s.getContainerName(&cosmosTenantItem{}): "/tenant",
	} {
		_, err := s.databaseClient.CreateContainer(ctx, azcosmos.ContainerProperties{
			ID:                     container,
			PartitionKeyDefinition: azcosmos.PartitionKeyDefinition{Paths: []string{path}},
		}, nil)
		if err != nil {
			t.Fatalf("create container %s: %v", container, err)
		}
	}
	return s
}

// Create defaults the partition key to the id, so a Get by id alone must look
// in every partition, not only the "" one (#295).
func TestCosmosDBGetByIdFindsItemInAnyPartition(t *testing.T) {
	s := newCosmosTestAdapter(t)
	if err := s.Create(&cosmosItem{Id: "c1", Name: "n"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var got cosmosItem
	if err := s.Get(&got, map[string]any{"id": "c1"}); err != nil {
		t.Fatalf("Get by id: %v", err)
	}
	if got.Name != "n" {
		t.Fatalf("got %+v; want the created item", got)
	}
}

func TestCosmosDBListWithoutPartitionKeySpansPartitions(t *testing.T) {
	s := newCosmosTestAdapter(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Create(&cosmosItem{Id: id, Name: "n"}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	var got []cosmosItem
	if _, err := s.List(&got, "id", map[string]any{"name": "n"}, 10, ""); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List returned %d items; want 3", len(got))
	}
}

func TestCosmosDBQuerySpansPartitions(t *testing.T) {
	s := newCosmosTestAdapter(t)
	for _, id := range []string{"a", "b"} {
		if err := s.Create(&cosmosItem{Id: id, Name: "n"}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	var got []cosmosItem
	if _, err := s.Query(&got, `SELECT * FROM c WHERE c.name = "n"`, 10, ""); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Query returned %d items; want 2", len(got))
	}
}

// WithPartitionKeyField names the partition key field and leaves the value on
// the item: Create reads it off the item, and every other operation works with
// the field alone.
func TestCosmosDBPartitionKeyFieldReadsValueOffItem(t *testing.T) {
	s := newCosmosTestAdapter(t)
	tenant := WithPartitionKeyField("tenant")
	ctx := context.Background()

	if err := s.Create(&cosmosTenantItem{Id: "t1", Tenant: "acme", Name: "n"}, tenant); err != nil {
		t.Fatalf("Create: %v", err)
	}
	container, err := s.databaseClient.NewContainer(s.getContainerName(&cosmosTenantItem{}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := container.ReadItem(ctx, azcosmos.NewPartitionKeyString("acme"), "t1", nil)
	if err != nil {
		t.Fatalf("the item is not in the acme partition: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw.Value, &stored); err != nil {
		t.Fatal(err)
	}
	if _, added := stored["pk"]; added {
		t.Fatalf("stored %v; want no pk property added next to tenant", stored)
	}

	var got cosmosTenantItem
	if err := s.Get(&got, map[string]any{"id": "t1"}, tenant); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := s.Update(&cosmosTenantItem{Id: "t1", Tenant: "acme", Name: "renamed"}, map[string]any{"id": "t1"}, tenant); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := s.Get(&got, map[string]any{"id": "t1"}, WithPartitionKey("tenant", "acme")); err != nil || got.Name != "renamed" {
		t.Fatalf("Get after Update = %+v, %v; want renamed", got, err)
	}
	var page []cosmosTenantItem
	if _, err := s.List(&page, "id", map[string]any{"name": "renamed"}, 10, "", tenant); err != nil || len(page) != 1 {
		t.Fatalf("List = %d items, %v; want 1", len(page), err)
	}
	if err := s.Delete(&cosmosTenantItem{}, map[string]any{"id": "t1", "tenant": "acme"}, tenant); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Get(&got, map[string]any{"id": "t1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v; want ErrNotFound", err)
	}
}

func TestCosmosDBCountNeedsPartitionKeyValue(t *testing.T) {
	s := newCosmosTestAdapter(t)
	_, err := s.Count(&cosmosTenantItem{}, nil, WithPartitionKeyField("tenant"))
	if err == nil || !strings.Contains(err.Error(), "use WithPartitionKey") {
		t.Fatalf("Count = %v; want an error naming WithPartitionKey", err)
	}
}

// Two callers read the same item and each change a different field. With
// WithFields, neither write reverts the other.
func TestCosmosDBUpdateWithFieldsKeepsConcurrentEdit(t *testing.T) {
	s := newCosmosTestAdapter(t)
	if err := s.Create(&cosmosItem{Id: "c1", Name: "original", Color: "red"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var first, second cosmosItem
	for _, read := range []*cosmosItem{&first, &second} {
		if err := s.Get(read, map[string]any{"id": "c1"}); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	first.Name = "renamed"
	second.Color = "blue"
	if err := s.Update(&first, map[string]any{"id": "c1"}, WithFields("name")); err != nil {
		t.Fatalf("Update name: %v", err)
	}
	if err := s.Update(&second, map[string]any{"id": "c1"}, WithFields("color")); err != nil {
		t.Fatalf("Update color: %v", err)
	}
	var got cosmosItem
	if err := s.Get(&got, map[string]any{"id": "c1"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "renamed" || got.Color != "blue" {
		t.Fatalf("got %+v; want both edits kept", got)
	}
}

func TestCosmosDBUpdateWithoutFieldsWritesEveryField(t *testing.T) {
	s := newCosmosTestAdapter(t)
	if err := s.Create(&cosmosItem{Id: "c1", Name: "original", Color: "red"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Update(&cosmosItem{Id: "c1", Name: "renamed"}, map[string]any{"id": "c1"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	var got cosmosItem
	if err := s.Get(&got, map[string]any{"id": "c1"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "renamed" || got.Color != "" {
		t.Fatalf("got %+v; want the zero color written, as on SQL and DynamoDB", got)
	}
}

func TestCosmosDBUpdateClearsOmitemptyField(t *testing.T) {
	for name, opts := range map[string][]Option{
		"without WithFields": nil,
		"with WithFields":    {WithFields("note")},
	} {
		t.Run(name, func(t *testing.T) {
			s := newCosmosTestAdapter(t)
			if err := s.Create(&cosmosItem{Id: "c1", Name: "n", Note: "keep me"}); err != nil {
				t.Fatalf("Create: %v", err)
			}
			if err := s.Update(&cosmosItem{Id: "c1", Name: "n"}, map[string]any{"id": "c1"}, opts...); err != nil {
				t.Fatalf("Update: %v", err)
			}
			var got cosmosItem
			if err := s.Get(&got, map[string]any{"id": "c1"}); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Note != "" || got.Name != "n" {
				t.Fatalf("got %+v; want the note cleared and the name kept", got)
			}
		})
	}
}

func TestCosmosDBUpdateRejectsUnknownField(t *testing.T) {
	s := newCosmosTestAdapter(t)
	if err := s.Create(&cosmosItem{Id: "c1", Name: "n"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := s.Update(&cosmosItem{Id: "c1"}, map[string]any{"id": "c1"}, WithFields("nmae"))
	if err == nil || !strings.Contains(err.Error(), `unknown field "nmae"`) {
		t.Fatalf("err = %v; want unknown field", err)
	}
}

// A property the struct does not declare, written by another service or an
// older version of this one, survives an Update.
func TestCosmosDBUpdateKeepsPropertiesAbsentFromTheItem(t *testing.T) {
	s := newCosmosTestAdapter(t)
	ctx := context.Background()
	container, err := s.databaseClient.NewContainer(s.getContainerName(&cosmosItem{}))
	if err != nil {
		t.Fatal(err)
	}
	doc := []byte(`{"id":"c1","pk":"c1","name":"n","legacy":"still here"}`)
	if _, err := container.CreateItem(ctx, azcosmos.NewPartitionKeyString("c1"), doc, nil); err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if err := s.Update(&cosmosItem{Id: "c1", Pk: "c1", Name: "renamed"}, map[string]any{"id": "c1"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	raw, err := container.ReadItem(ctx, azcosmos.NewPartitionKeyString("c1"), "c1", nil)
	if err != nil {
		t.Fatalf("ReadItem: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw.Value, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["legacy"] != "still here" || stored["name"] != "renamed" {
		t.Fatalf("stored %v; want legacy kept and name renamed", stored)
	}
}

func TestCosmosDBUpdateDoesNotCreateMissingItem(t *testing.T) {
	s := newCosmosTestAdapter(t)
	err := s.Update(&cosmosItem{Id: "ghost", Name: "n"}, map[string]any{"id": "ghost"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update of a missing item = %v; want ErrNotFound", err)
	}
}

// Writers that race on one item, each changing its own field, all succeed and
// all land: each replace is conditioned on the ETag it read, and a writer that
// lost the race applies its field again on top of the winner.
func TestCosmosDBConcurrentUpdatesWithFieldsAllLand(t *testing.T) {
	s := newCosmosTestAdapter(t)
	if err := s.Create(&cosmosItem{Id: "c1", Name: "n", Color: "c"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				errs <- s.Update(&cosmosItem{Id: "c1", Name: "renamed"}, map[string]any{"id": "c1"}, WithFields("name"))
			} else {
				errs <- s.Update(&cosmosItem{Id: "c1", Color: "blue"}, map[string]any{"id": "c1"}, WithFields("color"))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Update: %v", err)
		}
	}
	var got cosmosItem
	if err := s.Get(&got, map[string]any{"id": "c1"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "renamed" || got.Color != "blue" {
		t.Fatalf("got %+v; want both fields written", got)
	}
}

// Update addresses the item by its id; the filter is a condition on that item,
// never a way to pick another document.
func TestCosmosDBUpdateTargetsTheItemNotTheFirstFilterMatch(t *testing.T) {
	s := newCosmosTestAdapter(t)
	for _, item := range []*cosmosItem{{Id: "a", Name: "A", Color: "red"}, {Id: "b", Name: "B", Color: "blue"}} {
		if err := s.Create(item); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	err := s.Update(&cosmosItem{Id: "b", Name: "B2", Color: "red"}, map[string]any{"color": "red"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update of b filtered on a color b lacks = %v; want ErrNotFound", err)
	}
	var a cosmosItem
	if err := s.Get(&a, map[string]any{"id": "a"}); err != nil {
		t.Fatalf("Get a: %v", err)
	}
	if a.Name != "A" {
		t.Fatalf("a = %+v; want it untouched", a)
	}
}

func TestCosmosDBUpdateWithoutIdIsAddressedByTheFilter(t *testing.T) {
	s := newCosmosTestAdapter(t)
	if err := s.Create(&cosmosItem{Id: "a", Name: "A"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Update(&cosmosItem{Name: "A2"}, map[string]any{"id": "a"}, WithFields("name")); err != nil {
		t.Fatalf("Update: %v", err)
	}
	var got cosmosItem
	if err := s.Get(&got, map[string]any{"id": "a"}); err != nil || got.Name != "A2" {
		t.Fatalf("got %+v, %v; want A2", got, err)
	}
}

// A service may authorize by the filter's id and take the item from a request
// body. An item id that differs from the filter's must match nothing, never
// retarget the write to the body's id.
func TestCosmosDBUpdateWithMismatchedIdsMatchesNothing(t *testing.T) {
	s := newCosmosTestAdapter(t)
	for _, item := range []*cosmosItem{{Id: "mine", Name: "M"}, {Id: "theirs", Name: "T"}} {
		if err := s.Create(item); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	err := s.Update(&cosmosItem{Id: "theirs", Name: "hijacked"}, map[string]any{"id": "mine"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update = %v; want ErrNotFound", err)
	}
	for id, want := range map[string]string{"mine": "M", "theirs": "T"} {
		var got cosmosItem
		if err := s.Get(&got, map[string]any{"id": id}); err != nil || got.Name != want {
			t.Fatalf("%s = %+v, %v; want name %s untouched", id, got, err, want)
		}
	}
}
