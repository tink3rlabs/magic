package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
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
