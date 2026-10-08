package storage

import (
	"context"
	"strings"
	"testing"
)

// Pure helpers on *CosmosDBAdapter that do no network I/O. All
// of these are invoked per-operation in the real code path, so
// making sure their edge cases are nailed down covers a
// meaningful portion of the adapter without standing up an
// Azure Cosmos emulator.

type cosmosSampleItem struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type cosmosPascalItem struct {
	Id string `json:"id"`
}

func TestCosmosDBGetContainerNameSnakeCasePlural(t *testing.T) {
	s := &CosmosDBAdapter{}
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"single-word", cosmosSampleItem{}, "cosmos_sample_items"},
		{"pointer", &cosmosSampleItem{}, "cosmos_sample_items"},
		{"two-word", cosmosPascalItem{}, "cosmos_pascal_items"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.getContainerName(tc.in); got != tc.want {
				t.Fatalf("getContainerName(%T) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCosmosDBItemToMapRoundTripsJSONFields(t *testing.T) {
	s := &CosmosDBAdapter{}
	item := cosmosSampleItem{Id: "42", Name: "alpha"}

	m := s.itemToMap(item)
	if m["id"] != "42" {
		t.Fatalf("id = %v; want %q", m["id"], "42")
	}
	if m["name"] != "alpha" {
		t.Fatalf("name = %v; want %q", m["name"], "alpha")
	}
}

func TestCosmosDBItemToMapReturnsEmptyForUnmarshalableInput(t *testing.T) {
	s := &CosmosDBAdapter{}
	// A channel cannot be JSON-marshaled. itemToMap swallows the
	// marshal error and returns an empty map; covering that path
	// documents the defensive behaviour.
	m := s.itemToMap(make(chan int))
	if len(m) != 0 {
		t.Fatalf("expected empty map for unmarshalable input, got %v", m)
	}
}

func TestCosmosDBBuildPartitionKeyAbsentReturnsEmpty(t *testing.T) {
	s := &CosmosDBAdapter{}
	got, err := s.buildPartitionKey(map[string]any{})
	if err != nil {
		t.Fatalf("buildPartitionKey: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty pk when no pk_field specified, got %q", got)
	}
}

func TestCosmosDBBuildPartitionKeyPresentReturnsValueString(t *testing.T) {
	s := &CosmosDBAdapter{}
	got, err := s.buildPartitionKey(map[string]any{
		"pk_field": "tenant",
		"pk_value": 42,
	})
	if err != nil {
		t.Fatalf("buildPartitionKey: %v", err)
	}
	if got != "42" {
		t.Fatalf("pk = %q; want %q", got, "42")
	}
}

func TestCosmosDBBuildPartitionKeyFieldWithoutValueIsError(t *testing.T) {
	s := &CosmosDBAdapter{}
	_, err := s.buildPartitionKey(map[string]any{"pk_field": "tenant"})
	if err == nil {
		t.Fatalf("expected error when pk_field is set without pk_value")
	}
}

func TestCosmosDBBuildPartitionKeyNonStringFieldIsError(t *testing.T) {
	s := &CosmosDBAdapter{}
	_, err := s.buildPartitionKey(map[string]any{"pk_field": 5})
	if err == nil {
		t.Fatalf("expected error when pk_field is not a string")
	}
}

func TestCosmosDBBuildPartitionKeyEmptyStringFieldIsError(t *testing.T) {
	s := &CosmosDBAdapter{}
	_, err := s.buildPartitionKey(map[string]any{"pk_field": ""})
	if err == nil {
		t.Fatalf("expected error when pk_field is an empty string")
	}
}

func TestCosmosDBGetPartitionKeyFieldNameDefaultsToPk(t *testing.T) {
	s := &CosmosDBAdapter{}
	if got := s.getPartitionKeyFieldName(map[string]any{}); got != "pk" {
		t.Fatalf("default pk field = %q; want %q", got, "pk")
	}
}

func TestCosmosDBGetPartitionKeyFieldNameUsesCustomField(t *testing.T) {
	s := &CosmosDBAdapter{}
	got := s.getPartitionKeyFieldName(map[string]any{"pk_field": "tenant_id"})
	if got != "tenant_id" {
		t.Fatalf("pk field = %q; want %q", got, "tenant_id")
	}
}

func TestCosmosDBGetPartitionKeyFieldNameFallsBackForNonStringField(t *testing.T) {
	s := &CosmosDBAdapter{}
	got := s.getPartitionKeyFieldName(map[string]any{"pk_field": 12})
	if got != "pk" {
		t.Fatalf("pk field = %q; want fallback %q", got, "pk")
	}
}

func TestCosmosDBBuildFilterScalarCondition(t *testing.T) {
	s := &CosmosDBAdapter{}
	idx := 1
	clause, params := s.buildFilter(map[string]any{"id": "42"}, &idx)

	if clause != "c.id = @param1" {
		t.Fatalf("clause = %q; want %q", clause, "c.id = @param1")
	}
	if len(params) != 1 {
		t.Fatalf("len(params) = %d; want 1", len(params))
	}
	if params[0].Name != "@param1" || params[0].Value != "42" {
		t.Fatalf("param = %+v; want @param1=42", params[0])
	}
	if idx != 2 {
		t.Fatalf("paramIndex after one scalar = %d; want 2", idx)
	}
}

func TestCosmosDBBuildFilterSliceExpandsToInClause(t *testing.T) {
	s := &CosmosDBAdapter{}
	idx := 1
	clause, params := s.buildFilter(
		map[string]any{"status": []string{"active", "pending"}},
		&idx,
	)
	if !strings.HasPrefix(clause, "c.status IN (") {
		t.Fatalf("expected IN clause, got %q", clause)
	}
	if len(params) != 2 {
		t.Fatalf("len(params) = %d; want 2", len(params))
	}
	// Each element consumes one paramIndex slot.
	if idx != 3 {
		t.Fatalf("paramIndex = %d; want 3", idx)
	}
}

func TestCosmosDBTrivialGettersAndUnsupportedOps(t *testing.T) {
	// databaseName is a plain field on the struct so we can
	// exercise GetSchemaName without standing up a Cosmos
	// client.
	s := &CosmosDBAdapter{databaseName: "demo"}

	if got := s.GetType(); got != COSMOSDB {
		t.Fatalf("GetType = %q; want %q", got, COSMOSDB)
	}
	if got := s.GetProvider(); got != COSMOSDB_PROVIDER {
		t.Fatalf("GetProvider = %q; want %q", got, COSMOSDB_PROVIDER)
	}
	if got := s.GetSchemaName(); got != "demo" {
		t.Fatalf("GetSchemaName = %q; want %q", got, "demo")
	}

	// CreateSchema is a compatibility no-op; the rest are
	// documented as unsupported and must surface that as
	// errors (and not a silent nil).
	if err := s.CreateSchema(); err != nil {
		t.Fatalf("CreateSchema must be a no-op, got %v", err)
	}
	if err := s.Execute("SELECT 1"); err == nil {
		t.Fatalf("Execute must return an unsupported error")
	}
	if err := s.CreateMigrationTable(); err == nil {
		t.Fatalf("CreateMigrationTable must return an error")
	}
	if err := s.UpdateMigrationTable(1, "x", "y"); err == nil {
		t.Fatalf("UpdateMigrationTable must return an error")
	}
	if _, err := s.GetLatestMigration(); err == nil {
		t.Fatalf("GetLatestMigration must return an error")
	}
}

func TestCosmosDBBuildFilterMixedConditionsAreAndJoined(t *testing.T) {
	s := &CosmosDBAdapter{}
	idx := 1
	clause, _ := s.buildFilter(map[string]any{
		"id":     "42",
		"status": []string{"a", "b"},
	}, &idx)

	if !strings.Contains(clause, " AND ") {
		t.Fatalf("expected clauses joined by AND, got %q", clause)
	}
}

var cosmosCountPartition = map[string]any{"pk_field": "pk", "pk_value": "tenant-1"}

func TestCosmosDBBuildCountQueryRequiresPartitionKey(t *testing.T) {
	s := &CosmosDBAdapter{}
	cases := []struct {
		name   string
		filter map[string]any
		params map[string]any
	}{
		{"no filter, no params", nil, map[string]any{}},
		{"filter, no params", map[string]any{"status": "active"}, map[string]any{}},
		{"pk_value without pk_field", nil, map[string]any{"pk_value": "tenant-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := s.buildCountQuery(tc.filter, tc.params)
			if err == nil || !strings.Contains(err.Error(), "requires pk_field and pk_value") {
				t.Fatalf("err = %v; want a partition key required error", err)
			}
		})
	}
}

func TestCosmosDBBuildCountQueryPartitionOnlyCountsPartition(t *testing.T) {
	s := &CosmosDBAdapter{}
	query, params, err := s.buildCountQuery(nil, map[string]any{"pk_field": "tenant", "pk_value": "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query != "SELECT VALUE COUNT(1) FROM c WHERE c.tenant = @param1" {
		t.Fatalf("query = %q", query)
	}
	if len(params) != 1 || params[0].Name != "@param1" || params[0].Value != "t1" {
		t.Fatalf("params = %+v; want @param1=t1", params)
	}
}

func TestCosmosDBBuildCountQueryScalarFilterWithPartition(t *testing.T) {
	s := &CosmosDBAdapter{}
	query, params, err := s.buildCountQuery(map[string]any{"status": "active"}, cosmosCountPartition)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "SELECT VALUE COUNT(1) FROM c WHERE c.status = @param1 AND c.pk = @param2"
	if query != want {
		t.Fatalf("query = %q; want %q", query, want)
	}
	if len(params) != 2 {
		t.Fatalf("len(params) = %d; want 2", len(params))
	}
	if params[0].Name != "@param1" || params[0].Value != "active" {
		t.Fatalf("params[0] = %+v; want @param1=active", params[0])
	}
	if params[1].Name != "@param2" || params[1].Value != "tenant-1" {
		t.Fatalf("params[1] = %+v; want @param2=tenant-1", params[1])
	}
}

func TestCosmosDBBuildCountQueryMultipleKeysAreAndJoined(t *testing.T) {
	s := &CosmosDBAdapter{}
	query, params, err := s.buildCountQuery(
		map[string]any{"status": "active", "owner": "me"},
		cosmosCountPartition,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(query, "SELECT VALUE COUNT(1) FROM c WHERE ") {
		t.Fatalf("query = %q; want a WHERE clause", query)
	}
	if !strings.Contains(query, "c.status = ") || !strings.Contains(query, "c.owner = ") || !strings.HasSuffix(query, "AND c.pk = @param3") {
		t.Fatalf("query = %q; want both keys AND-joined, then the partition", query)
	}
	if len(params) != 3 {
		t.Fatalf("len(params) = %d; want 3", len(params))
	}
}

func TestCosmosDBBuildCountQuerySliceFilterUsesInClause(t *testing.T) {
	s := &CosmosDBAdapter{}
	query, params, err := s.buildCountQuery(
		map[string]any{"status": []string{"active", "pending"}},
		cosmosCountPartition,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "SELECT VALUE COUNT(1) FROM c WHERE c.status IN (@param1, @param2) AND c.pk = @param3"
	if query != want {
		t.Fatalf("query = %q; want %q", query, want)
	}
	if len(params) != 3 {
		t.Fatalf("len(params) = %d; want 3", len(params))
	}
}

func TestCosmosDBBuildCountQueryRejectsUnsafeFilterKeys(t *testing.T) {
	s := &CosmosDBAdapter{}
	for _, key := range []string{"id = 1 OR 1=1 --", "a b", "a.b", "", "1abc", "a-b"} {
		_, _, err := s.buildCountQuery(map[string]any{key: "x"}, cosmosCountPartition)
		if err == nil || !strings.Contains(err.Error(), "invalid filter key") {
			t.Fatalf("key %q: err = %v; want an invalid filter key error", key, err)
		}
	}
}

func TestCosmosDBBuildCountQueryRejectsUnsafePartitionField(t *testing.T) {
	s := &CosmosDBAdapter{}
	_, _, err := s.buildCountQuery(nil, map[string]any{"pk_field": "a b", "pk_value": "x"})
	if err == nil || !strings.Contains(err.Error(), "invalid partition key field") {
		t.Fatalf("err = %v; want an invalid partition key field error", err)
	}
}

func TestCosmosDBBuildCountQueryPartitionFieldWithoutValueIsError(t *testing.T) {
	s := &CosmosDBAdapter{}
	if _, _, err := s.buildCountQuery(nil, map[string]any{"pk_field": "tenant"}); err == nil {
		t.Fatalf("expected an error when pk_field has no pk_value")
	}
}

func TestCosmosDBCountContextReturnsErrorsInsteadOfZero(t *testing.T) {
	s := &CosmosDBAdapter{}
	ctx := context.Background()

	cases := []struct {
		name   string
		dest   any
		filter map[string]any
		params []map[string]any
	}{
		{"nil destination", nil, nil, []map[string]any{cosmosCountPartition}},
		{"missing partition key", &cosmosSampleItem{}, nil, nil},
		{"invalid filter key", &cosmosSampleItem{}, map[string]any{"a b": 1}, []map[string]any{cosmosCountPartition}},
		{"pk_field without pk_value", &cosmosSampleItem{}, nil, []map[string]any{{"pk_field": "tenant"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := s.CountContext(ctx, tc.dest, tc.filter, tc.params...)
			if err == nil {
				t.Fatalf("CountContext returned (%d, nil); want an error", n)
			}
			if n != 0 {
				t.Fatalf("count = %d on error; want 0", n)
			}
		})
	}
}
