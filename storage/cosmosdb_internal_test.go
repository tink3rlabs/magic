package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
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
	clause, params, err := s.buildFilter(map[string]any{"id": "42"}, &idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != `c["id"] = @param1` {
		t.Fatalf("clause = %q; want %q", clause, `c["id"] = @param1`)
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
	clause, params, err := s.buildFilter(
		map[string]any{"status": []string{"active", "pending"}},
		&idx,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(clause, `c["status"] IN (`) {
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
	clause, _, err := s.buildFilter(map[string]any{
		"id":     "42",
		"status": []string{"a", "b"},
	}, &idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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
		{"empty pk_value", nil, map[string]any{"pk_field": "tenant", "pk_value": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := s.buildCountQuery(tc.filter, tc.params)
			if err == nil || !strings.Contains(err.Error(), "requires a non-empty pk_field and pk_value") {
				t.Fatalf("err = %v; want a partition key required error", err)
			}
		})
	}
}

func TestCosmosDBBuildCountQueryPartitionOnlyCountsPartition(t *testing.T) {
	s := &CosmosDBAdapter{}
	query, params, _, err := s.buildCountQuery(nil, map[string]any{"pk_field": "tenant", "pk_value": "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if query != `SELECT VALUE COUNT(1) FROM c WHERE c["tenant"] = @param1` {
		t.Fatalf("query = %q", query)
	}
	if len(params) != 1 || params[0].Name != "@param1" || params[0].Value != "t1" {
		t.Fatalf("params = %+v; want @param1=t1", params)
	}
}

func TestCosmosDBBuildCountQueryScalarFilterWithPartition(t *testing.T) {
	s := &CosmosDBAdapter{}
	query, params, _, err := s.buildCountQuery(map[string]any{"status": "active"}, cosmosCountPartition)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT VALUE COUNT(1) FROM c WHERE c["status"] = @param1 AND c["pk"] = @param2`
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
	query, params, _, err := s.buildCountQuery(
		map[string]any{"status": "active", "owner": "me"},
		cosmosCountPartition,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(query, "SELECT VALUE COUNT(1) FROM c WHERE ") {
		t.Fatalf("query = %q; want a WHERE clause", query)
	}
	if !strings.Contains(query, `c["status"] = `) || !strings.Contains(query, `c["owner"] = `) || !strings.HasSuffix(query, `AND c["pk"] = @param3`) {
		t.Fatalf("query = %q; want both keys AND-joined, then the partition", query)
	}
	if len(params) != 3 {
		t.Fatalf("len(params) = %d; want 3", len(params))
	}
}

func TestCosmosDBBuildCountQuerySliceFilterUsesInClause(t *testing.T) {
	s := &CosmosDBAdapter{}
	query, params, _, err := s.buildCountQuery(
		map[string]any{"status": []string{"active", "pending"}},
		cosmosCountPartition,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT VALUE COUNT(1) FROM c WHERE c["status"] IN (@param1, @param2) AND c["pk"] = @param3`
	if query != want {
		t.Fatalf("query = %q; want %q", query, want)
	}
	if len(params) != 3 {
		t.Fatalf("len(params) = %d; want 3", len(params))
	}
}

func TestCosmosDBBuildCountQueryRejectsUnsafeFilterKeys(t *testing.T) {
	s := &CosmosDBAdapter{}
	for _, key := range []string{"id = 1 OR 1=1 --", "a b", "", "1abc", "a-b", "a.", ".a", "a..b", "a.1b"} {
		_, _, _, err := s.buildCountQuery(map[string]any{key: "x"}, cosmosCountPartition)
		if err == nil || !strings.Contains(err.Error(), "invalid filter key") {
			t.Fatalf("key %q: err = %v; want an invalid filter key error", key, err)
		}
	}
}

func TestCosmosDBBuildCountQueryRejectsUnsafePartitionField(t *testing.T) {
	s := &CosmosDBAdapter{}
	// A dotted pk_field is rejected too: Create stores the partition value
	// under the literal top-level key, so a nested lookup would count 0.
	for _, field := range []string{"a b", `a"]`, "meta.tenant"} {
		_, _, _, err := s.buildCountQuery(nil, map[string]any{"pk_field": field, "pk_value": "x"})
		if err == nil || !strings.Contains(err.Error(), "invalid partition key field") {
			t.Fatalf("pk_field %q: err = %v; want an invalid partition key field error", field, err)
		}
	}
}

func TestCosmosDBBuildCountQueryPartitionFieldWithoutValueIsError(t *testing.T) {
	s := &CosmosDBAdapter{}
	if _, _, _, err := s.buildCountQuery(nil, map[string]any{"pk_field": "tenant"}); err == nil {
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

func TestCosmosDBBuildCountQueryReturnsPartitionKey(t *testing.T) {
	s := &CosmosDBAdapter{}
	_, _, pk, err := s.buildCountQuery(nil, map[string]any{"pk_field": "tenant", "pk_value": 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pk != "7" {
		t.Fatalf("pk = %q; want %q", pk, "7")
	}
}

func TestCosmosDBBuildCountQueryAllowsNestedFilterPaths(t *testing.T) {
	s := &CosmosDBAdapter{}
	query, _, _, err := s.buildCountQuery(
		map[string]any{"address.city": "Paris"},
		map[string]any{"pk_field": "tenant", "pk_value": "t1"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT VALUE COUNT(1) FROM c WHERE c["address"]["city"] = @param1 AND c["tenant"] = @param2`
	if query != want {
		t.Fatalf("query = %q; want %q", query, want)
	}
}

func TestCosmosDBBuildCountQueryEmptySliceMatchesNothing(t *testing.T) {
	s := &CosmosDBAdapter{}
	query, params, _, err := s.buildCountQuery(
		map[string]any{"status": []string{}},
		cosmosCountPartition,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `SELECT VALUE COUNT(1) FROM c WHERE false AND c["pk"] = @param1`
	if query != want {
		t.Fatalf("query = %q; want %q", query, want)
	}
	if len(params) != 1 || params[0].Value != "tenant-1" {
		t.Fatalf("params = %+v; want only the partition key", params)
	}
}

func TestCosmosDBBuildFilterEmptySliceIsFalse(t *testing.T) {
	s := &CosmosDBAdapter{}
	idx := 1
	clause, params, err := s.buildFilter(map[string]any{"status": []string{}}, &idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clause != "false" {
		t.Fatalf("clause = %q; want %q", clause, "false")
	}
	if len(params) != 0 || idx != 1 {
		t.Fatalf("params = %+v, idx = %d; want no params and idx unchanged", params, idx)
	}
}

// fakeCountPager returns a pager that yields pages in order, or err on the
// page at errAt (-1 for never).
func fakeCountPager(pages [][][]byte, errAt int) *runtime.Pager[azcosmos.QueryItemsResponse] {
	i := 0
	return runtime.NewPager(runtime.PagingHandler[azcosmos.QueryItemsResponse]{
		More: func(azcosmos.QueryItemsResponse) bool { return i < len(pages) },
		Fetcher: func(context.Context, *azcosmos.QueryItemsResponse) (azcosmos.QueryItemsResponse, error) {
			if i == errAt {
				return azcosmos.QueryItemsResponse{}, errors.New("boom")
			}
			page := azcosmos.QueryItemsResponse{Items: pages[i]}
			i++
			return page, nil
		},
	})
}

func TestCosmosDBSumCountPagesSumsEveryPage(t *testing.T) {
	pager := fakeCountPager([][][]byte{
		{[]byte("3")},
		{},
		{[]byte("4"), []byte("5")},
	}, -1)
	n, err := sumCountPages(context.Background(), pager)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 12 {
		t.Fatalf("count = %d; want 12", n)
	}
}

func TestCosmosDBSumCountPagesErrors(t *testing.T) {
	cases := []struct {
		name  string
		pages [][][]byte
		errAt int
		want  string
	}{
		{"query failure", [][][]byte{{[]byte("3")}, {[]byte("4")}}, 1, "failed to execute count query"},
		{"non-numeric result", [][][]byte{{[]byte(`{"n":1}`)}}, -1, "failed to parse count result"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := sumCountPages(context.Background(), fakeCountPager(tc.pages, tc.errAt))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v; want %q", err, tc.want)
			}
			if n != 0 {
				t.Fatalf("count = %d on error; want 0", n)
			}
		})
	}
}

func TestCosmosDBBuildFilterQuotesKeywordFields(t *testing.T) {
	s := &CosmosDBAdapter{}
	idx := 1
	clause, _, err := s.buildFilter(map[string]any{"order": 1}, &idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clause != `c["order"] = @param1` {
		t.Fatalf("clause = %q; want %q", clause, `c["order"] = @param1`)
	}
}

func TestCosmosDBBuildFilterNilMatchesNullOrMissing(t *testing.T) {
	s := &CosmosDBAdapter{}
	idx := 1
	clause, params, err := s.buildFilter(map[string]any{"deleted_at": nil}, &idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `(NOT IS_DEFINED(c["deleted_at"]) OR IS_NULL(c["deleted_at"]))`
	if clause != want {
		t.Fatalf("clause = %q; want %q", clause, want)
	}
	if len(params) != 0 || idx != 1 {
		t.Fatalf("params = %+v, idx = %d; want no params and idx unchanged", params, idx)
	}
}

func TestCosmosDBBuildFilterRejectsUnsafeKeys(t *testing.T) {
	s := &CosmosDBAdapter{}
	for _, key := range []string{`id"] OR 1=1 --`, "a b", "", "a..b"} {
		idx := 1
		_, _, err := s.buildFilter(map[string]any{key: "x"}, &idx)
		if err == nil || !strings.Contains(err.Error(), "invalid filter key") {
			t.Fatalf("key %q: err = %v; want an invalid filter key error", key, err)
		}
	}
}

func TestCosmosDBGetContextRejectsUnsafeKeysBeforeQuerying(t *testing.T) {
	s := newFakeCosmosAdapter(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("GetContext sent a request for an invalid filter key")
		return nil, nil
	})
	var item cosmosSampleItem
	err := s.GetContext(context.Background(), &item, map[string]any{"a b": "x"})
	if err == nil || !strings.Contains(err.Error(), "invalid filter key") {
		t.Fatalf("err = %v; want an invalid filter key error", err)
	}
}

func TestCosmosDBListContextRejectsUnsafeKeysBeforeQuerying(t *testing.T) {
	s := newFakeCosmosAdapter(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("ListContext sent a request for an invalid filter key")
		return nil, nil
	})
	var items []cosmosSampleItem
	_, err := s.ListContext(context.Background(), &items, "id", map[string]any{"a b": "x"}, 10, "")
	if err == nil || !strings.Contains(err.Error(), "invalid filter key") {
		t.Fatalf("err = %v; want an invalid filter key error", err)
	}
}

// fakeCosmosTransport sends every request to do instead of the network.
type fakeCosmosTransport func(*http.Request) (*http.Response, error)

func (f fakeCosmosTransport) Do(req *http.Request) (*http.Response, error) { return f(req) }

func newFakeCosmosAdapter(t *testing.T, do fakeCosmosTransport) *CosmosDBAdapter {
	t.Helper()
	cred, err := azcosmos.NewKeyCredential(base64.StdEncoding.EncodeToString([]byte("test-key")))
	if err != nil {
		t.Fatalf("NewKeyCredential: %v", err)
	}
	client, err := azcosmos.NewClientWithKey("https://fake.documents.azure.com:443/", cred, &azcosmos.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: do,
			Retry:     policy.RetryOptions{MaxRetries: -1},
		},
	})
	if err != nil {
		t.Fatalf("NewClientWithKey: %v", err)
	}
	db, err := client.NewDatabase("db")
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	return &CosmosDBAdapter{client: client, databaseClient: db, databaseName: "db"}
}

func fakeCosmosJSON(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestCosmosDBCountContextSendsScopedQuery(t *testing.T) {
	var gotPath, gotPK string
	var gotBody struct {
		Query      string `json:"query"`
		Parameters []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"parameters"`
	}
	s := newFakeCosmosAdapter(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Path == "/" {
			// The SDK reads account properties before its first operation.
			return fakeCosmosJSON(req, `{"id":"fake","writableLocations":[],"readableLocations":[]}`), nil
		}
		gotPath = req.URL.Path
		gotPK = req.Header.Get("x-ms-documentdb-partitionkey")
		if err := json.NewDecoder(req.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return fakeCosmosJSON(req, `{"_rid":"x","Documents":[7],"_count":1}`), nil
	})

	n, err := s.CountContext(context.Background(), &cosmosSampleItem{}, map[string]any{"status": "active"}, cosmosCountPartition)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 7 {
		t.Fatalf("count = %d; want 7", n)
	}
	if gotPath != "/dbs/db/colls/cosmos_sample_items/docs" {
		t.Fatalf("path = %q; want the cosmos_sample_items docs feed", gotPath)
	}
	if gotPK != `["tenant-1"]` {
		t.Fatalf("partition key header = %q; want %q", gotPK, `["tenant-1"]`)
	}
	want := `SELECT VALUE COUNT(1) FROM c WHERE c["status"] = @param1 AND c["pk"] = @param2`
	if gotBody.Query != want {
		t.Fatalf("query = %q; want %q", gotBody.Query, want)
	}
	if len(gotBody.Parameters) != 2 || gotBody.Parameters[1].Value != "tenant-1" {
		t.Fatalf("parameters = %+v; want status then the partition value", gotBody.Parameters)
	}
}

func TestCosmosDBApplySessionTokenScopesByContainerAndPartition(t *testing.T) {
	s := &CosmosDBAdapter{sessionTokens: newSessionTokenStore()}
	first := "0:-1#11"
	second := "1:-1#22"

	s.setSessionToken("orders", "tenant-a", &first)
	s.setSessionToken("orders", "tenant-b", &second)

	queryOptions := &azcosmos.QueryOptions{}
	s.applySessionToken("orders", "tenant-a", queryOptions)
	if queryOptions.SessionToken == nil || *queryOptions.SessionToken != first {
		t.Fatalf("SessionToken for tenant-a = %v; want %q", queryOptions.SessionToken, first)
	}

	queryOptions = &azcosmos.QueryOptions{}
	s.applySessionToken("orders", "tenant-b", queryOptions)
	if queryOptions.SessionToken == nil || *queryOptions.SessionToken != second {
		t.Fatalf("SessionToken for tenant-b = %v; want %q", queryOptions.SessionToken, second)
	}

	queryOptions = &azcosmos.QueryOptions{}
	s.applySessionToken("orders", "tenant-c", queryOptions)
	if queryOptions.SessionToken != nil {
		t.Fatalf("partition with no write must not borrow another partition's token, got %q", *queryOptions.SessionToken)
	}
}

func TestCosmosDBApplySessionTokenAcrossPartitionsMergesRanges(t *testing.T) {
	s := &CosmosDBAdapter{sessionTokens: newSessionTokenStore()}
	for pk, token := range map[string]string{
		"tenant-a": "0:-1#11",
		"tenant-b": "0:-1#15", // same range as tenant-a, newer
		"tenant-c": "1:-1#7",
	} {
		s.setSessionToken("orders", pk, &token)
	}
	other := "0:-1#99"
	s.setSessionToken("invoices", "tenant-a", &other)

	queryOptions := &azcosmos.QueryOptions{}
	s.applySessionToken("orders", "", queryOptions)
	want := "0:-1#15,1:-1#7"
	if queryOptions.SessionToken == nil || *queryOptions.SessionToken != want {
		t.Fatalf("cross-partition SessionToken = %v; want %q", queryOptions.SessionToken, want)
	}
}

func TestCosmosDBApplySessionTokenKeepsExistingWhenNoneStored(t *testing.T) {
	s := &CosmosDBAdapter{sessionTokens: newSessionTokenStore()}
	queryOptions := &azcosmos.QueryOptions{SessionToken: strPtr("existing")}

	s.applySessionToken("orders", "tenant-a", queryOptions)
	if queryOptions.SessionToken == nil || *queryOptions.SessionToken != "existing" {
		t.Fatalf("expected existing SessionToken to remain unchanged, got %#v", queryOptions.SessionToken)
	}
}

func TestSessionTokenStoreExpiresAndEvictsOldest(t *testing.T) {
	now := time.Unix(0, 0)
	store := newSessionTokenStore()
	store.ttl = 5 * time.Second
	store.max = 2
	store.now = func() time.Time { return now }

	store.set("orders", "tenant-a", "0:1#12")
	store.set("orders", "tenant-b", "0:1#15")

	if got := store.get("orders", "tenant-a"); got != "0:1#12" {
		t.Fatalf("tenant-a token = %q; want %q", got, "0:1#12")
	}

	now = now.Add(6 * time.Second)
	if got := store.get("orders", "tenant-a"); got != "" {
		t.Fatalf("expired token should be gone, got %q", got)
	}

	store.set("orders", "tenant-c", "0:1#20")
	store.set("orders", "tenant-d", "0:1#21")
	store.set("orders", "tenant-e", "0:1#22")
	if got := store.get("orders", "tenant-c"); got != "" {
		t.Fatalf("evicted oldest token should be removed, got %q", got)
	}
	if got := store.get("orders", "tenant-d"); got != "0:1#21" {
		t.Fatalf("tenant-d token = %q; want %q", got, "0:1#21")
	}
	if got := store.order.Len(); got != 2 {
		t.Fatalf("store holds %d entries; want 2", got)
	}
}

func TestSessionTokenStoreUpdateMovesEntryToBackOfEviction(t *testing.T) {
	now := time.Unix(0, 0)
	store := newSessionTokenStore()
	store.max = 2
	store.now = func() time.Time { return now }

	store.set("orders", "tenant-a", "0:1#1")
	store.set("orders", "tenant-b", "0:1#2")
	store.set("orders", "tenant-a", "0:1#3") // tenant-b is now the least recently updated
	store.set("orders", "tenant-c", "0:1#4")

	if got := store.get("orders", "tenant-b"); got != "" {
		t.Fatalf("least recently updated token should be evicted, got %q", got)
	}
	if got := store.get("orders", "tenant-a"); got != "0:1#3" {
		t.Fatalf("tenant-a token = %q; want %q", got, "0:1#3")
	}
}

func TestSessionTokenStoreKeepsNewestValidToken(t *testing.T) {
	store := newSessionTokenStore()
	store.set("orders", "tenant-a", "0:1#12")
	store.set("orders", "tenant-a", "0:1#15")
	if got := store.get("orders", "tenant-a"); got != "0:1#15" {
		t.Fatalf("newer higher LSN should replace older token; got %q", got)
	}
	store.set("orders", "tenant-a", "bad-token")
	if got := store.get("orders", "tenant-a"); got != "0:1#15" {
		t.Fatalf("invalid token should not replace valid token; got %q", got)
	}
}

func TestSessionTokenStoreOlderTokenDoesNotReplaceNewer(t *testing.T) {
	store := newSessionTokenStore()
	store.set("orders", "tenant-a", "0:-1#15")
	store.set("orders", "tenant-a", "0:-1#12")
	if got := store.get("orders", "tenant-a"); got != "0:-1#15" {
		t.Fatalf("older token replaced newer: got %q", got)
	}
}

func TestSessionTokenStoreNewRangeReplaces(t *testing.T) {
	store := newSessionTokenStore()
	store.set("orders", "tenant-a", "0:-1#500")
	store.set("orders", "tenant-a", "3:-1#7") // after a split
	if got := store.get("orders", "tenant-a"); got != "3:-1#7" {
		t.Fatalf("token from new range not taken: got %q", got)
	}
}

func TestParseSessionToken(t *testing.T) {
	cases := []struct {
		token string
		want  sessionTokenRank
		ok    bool
	}{
		{"0:-1#12", sessionTokenRank{rangeID: "0", version: -1, lsn: 12}, true},
		{"0:1#12345#3=12340#4=12338", sessionTokenRank{rangeID: "0", version: 1, lsn: 12345}, true},
		{"7:2#9223372036854775807", sessionTokenRank{rangeID: "7", version: 2, lsn: 9223372036854775807}, true},
		{"bad-token", sessionTokenRank{}, false},
		{":1#12", sessionTokenRank{}, false},
		{"0:1", sessionTokenRank{}, false},
		{"0:x#12", sessionTokenRank{}, false},
	}
	for _, tc := range cases {
		got, ok := parseSessionToken(tc.token)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("parseSessionToken(%q) = %+v, %v; want %+v, %v", tc.token, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSessionTokenStoreMultiRegionKeepsNewestGlobalLSN(t *testing.T) {
	store := newSessionTokenStore()
	// The newer write has a higher global LSN but a lower trailing region LSN.
	store.set("orders", "tenant-a", "0:1#12340#4=12339")
	store.set("orders", "tenant-a", "0:1#12345#3=12340#4=12338")
	if got := store.get("orders", "tenant-a"); got != "0:1#12345#3=12340#4=12338" {
		t.Fatalf("newer multi-region token not kept: got %q", got)
	}
}

func TestCosmosDBRecordSessionTokenFromConflictError(t *testing.T) {
	s := &CosmosDBAdapter{sessionTokens: newSessionTokenStore()}
	h := http.Header{}
	h.Set("x-ms-session-token", "0:-1#42")
	resp := &http.Response{
		StatusCode: http.StatusConflict,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(`{"code":"Conflict"}`)),
		Request:    httptest.NewRequest(http.MethodPost, "https://acct.documents.azure.com/dbs/d/colls/orders/docs", nil),
	}
	s.recordSessionToken("orders", "tenant-a", nil, fmt.Errorf("create: %w", runtime.NewResponseError(resp)))
	if got := s.sessionTokens.get("orders", "tenant-a"); got != "0:-1#42" {
		t.Fatalf("token from 409 not stored: got %q", got)
	}
}

func TestCosmosDBRecordSessionTokenIgnoresErrorWithoutResponse(t *testing.T) {
	s := &CosmosDBAdapter{sessionTokens: newSessionTokenStore()}
	s.recordSessionToken("orders", "tenant-a", nil, errors.New("dial tcp: timeout"))
	if got := s.sessionTokens.get("orders", "tenant-a"); got != "" {
		t.Fatalf("expected no token, got %q", got)
	}
}

func TestCosmosDBSessionTokensConcurrentAccess(t *testing.T) {
	s := &CosmosDBAdapter{sessionTokens: newSessionTokenStore()}
	s.sessionTokens.max = 3 // force eviction while goroutines race
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pk := fmt.Sprintf("tenant-%d", i%5)
			tok := fmt.Sprintf("0:-1#%d", i)
			s.recordSessionToken("orders", pk, &tok, nil)
			s.applySessionToken("orders", pk, &azcosmos.QueryOptions{})
			s.applySessionToken("orders", "", &azcosmos.QueryOptions{})
		}(i)
	}
	wg.Wait()
}

func strPtr(s string) *string {
	return &s
}

// sessionTokenTransport answers the requests a CosmosDBAdapter makes against a
// real azcosmos.Client, so tests can check the headers that actually go out.
// Creates return createStatus and deletes return 204, both with the session
// token writeToken. Queries return queryDocuments (one item by default) and
// record the session token they were sent.
type sessionTokenTransport struct {
	createStatus   int
	writeToken     string
	queryDocuments string

	mu            sync.Mutex
	querySessions []string
}

func (f *sessionTokenTransport) Do(req *http.Request) (*http.Response, error) {
	status, body, header := http.StatusOK, "{}", http.Header{}
	header.Set("Content-Type", "application/json")
	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/":
		body = `{"id":"acct","writableLocations":[{"name":"East US","databaseAccountEndpoint":"https://acct.documents.azure.com:443/"}],` +
			`"readableLocations":[{"name":"East US","databaseAccountEndpoint":"https://acct.documents.azure.com:443/"}]}`
	case req.Header.Get("x-ms-documentdb-query") == "True":
		f.mu.Lock()
		f.querySessions = append(f.querySessions, req.Header.Get("x-ms-session-token"))
		f.mu.Unlock()
		docs := f.queryDocuments
		if docs == "" {
			docs = `[{"id":"1","pk":"a"}]`
		}
		body = `{"_rid":"r","Documents":` + docs + `}`
	case req.Method == http.MethodDelete:
		status, body = http.StatusNoContent, ""
		header.Set("x-ms-session-token", f.writeToken)
	case strings.HasSuffix(req.URL.Path, "/docs"):
		status = f.createStatus
		header.Set("x-ms-session-token", f.writeToken)
		body = `{"id":"1","pk":"a"}`
		if status == http.StatusConflict {
			body = `{"code":"Conflict","message":"Entity with the specified id already exists in the system."}`
		}
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func (f *sessionTokenTransport) lastQuerySession(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.querySessions) == 0 {
		t.Fatalf("no query was sent")
	}
	return f.querySessions[len(f.querySessions)-1]
}

// newSessionTokenAdapter returns a fake adapter, served by transport, that
// tracks session tokens.
func newSessionTokenAdapter(t *testing.T, transport *sessionTokenTransport) *CosmosDBAdapter {
	t.Helper()
	s := newFakeCosmosAdapter(t, transport.Do)
	s.sessionTokens = newSessionTokenStore()
	return s
}

func TestCosmosDBReadsSendSessionTokenFromWrite(t *testing.T) {
	cases := []struct {
		name         string
		createStatus int
		wantErr      bool
	}{
		{"after a successful create", http.StatusCreated, false},
		{"after a 409 conflict", http.StatusConflict, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport := &sessionTokenTransport{createStatus: tc.createStatus, writeToken: "0:-1#42"}
			s := newSessionTokenAdapter(t, transport)
			pkA := map[string]any{"pk_field": "pk", "pk_value": "a"}
			pkB := map[string]any{"pk_field": "pk", "pk_value": "b"}

			if err := s.Create(cosmosSampleItem{Id: "1"}, pkA); (err != nil) != tc.wantErr {
				t.Fatalf("Create err = %v; want error: %v", err, tc.wantErr)
			}

			var got cosmosSampleItem
			if err := s.Get(&got, map[string]any{"id": "1"}, pkA); err != nil {
				t.Fatalf("Get pk=a: %v", err)
			}
			if got := transport.lastQuerySession(t); got != "0:-1#42" {
				t.Fatalf("Get pk=a sent session token %q; want %q", got, "0:-1#42")
			}

			if err := s.Get(&got, map[string]any{"id": "1"}, pkB); err != nil {
				t.Fatalf("Get pk=b: %v", err)
			}
			if got := transport.lastQuerySession(t); got != "" {
				t.Fatalf("Get pk=b sent session token %q; want none", got)
			}

			var page []cosmosSampleItem
			if _, err := s.List(&page, "id", nil, 10, ""); err != nil {
				t.Fatalf("List: %v", err)
			}
			if got := transport.lastQuerySession(t); got != "0:-1#42" {
				t.Fatalf("List across partitions sent session token %q; want %q", got, "0:-1#42")
			}
		})
	}
}

func TestCosmosDBCountSendsSessionTokenFromWrite(t *testing.T) {
	transport := &sessionTokenTransport{createStatus: http.StatusCreated, writeToken: "0:-1#44", queryDocuments: `[1]`}
	s := newSessionTokenAdapter(t, transport)
	params := map[string]any{"pk_field": "pk", "pk_value": "a"}

	if err := s.Create(cosmosSampleItem{Id: "1"}, params); err != nil {
		t.Fatalf("Create: %v", err)
	}
	n, err := s.Count(&cosmosSampleItem{}, nil, params)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 1 {
		t.Fatalf("Count = %d; want 1", n)
	}
	if got := transport.lastQuerySession(t); got != "0:-1#44" {
		t.Fatalf("Count sent session token %q; want %q", got, "0:-1#44")
	}
}

func TestCosmosDBSessionTokensConfigTurnsTrackingOff(t *testing.T) {
	if newCosmosSessionTokenStore(map[string]string{"session_tokens": "false"}) != nil {
		t.Fatalf(`session_tokens "false" should turn tracking off`)
	}
	for _, v := range []string{"", "true", "not-a-bool"} {
		if newCosmosSessionTokenStore(map[string]string{"session_tokens": v}) == nil {
			t.Fatalf("session_tokens %q should leave tracking on", v)
		}
	}

	transport := &sessionTokenTransport{createStatus: http.StatusCreated, writeToken: "0:-1#42"}
	s := newSessionTokenAdapter(t, transport)
	s.sessionTokens = nil
	params := map[string]any{"pk_field": "pk", "pk_value": "a"}
	if err := s.Create(cosmosSampleItem{Id: "1"}, params); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var got cosmosSampleItem
	if err := s.Get(&got, map[string]any{"id": "1"}, params); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := transport.lastQuerySession(t); got != "" {
		t.Fatalf("Get sent session token %q with tracking off; want none", got)
	}
}

func TestSessionTokenStoreRangeTokenOutlivesEvictedPartition(t *testing.T) {
	store := newSessionTokenStore()
	store.max = 2
	store.set("orders", "tenant-a", "0:-1#50")
	store.set("orders", "tenant-b", "0:-1#10")
	store.set("orders", "tenant-c", "1:-1#5") // evicts tenant-a

	if got := store.get("orders", "tenant-a"); got != "" {
		t.Fatalf("tenant-a should be evicted, got %q", got)
	}
	// Range 0 still holds tenant-a's newer write, so queries across
	// partitions keep seeing it.
	if got, want := store.getForContainer("orders"), "0:-1#50,1:-1#5"; got != want {
		t.Fatalf("getForContainer = %q; want %q", got, want)
	}
}

func TestSessionTokenStoreRangeTokensDroppedWithContainer(t *testing.T) {
	now := time.Unix(0, 0)
	store := newSessionTokenStore()
	store.ttl = 5 * time.Second
	store.now = func() time.Time { return now }
	store.set("orders", "tenant-a", "0:-1#50")

	now = now.Add(6 * time.Second)
	if got := store.getForContainer("orders"); got != "" {
		t.Fatalf("expired range token returned: %q", got)
	}
	store.set("invoices", "tenant-a", "0:-1#1") // purges the expired orders entry
	if _, ok := store.byRange["orders"]; ok {
		t.Fatalf("range tokens for a container with no entries should be dropped")
	}
}

func TestCosmosDBGetAfterDeleteSendsDeleteSessionToken(t *testing.T) {
	transport := &sessionTokenTransport{writeToken: "0:-1#43"}
	s := newSessionTokenAdapter(t, transport)
	params := map[string]any{"pk_field": "pk", "pk_value": "a"}

	if err := s.Delete(cosmosSampleItem{}, map[string]any{"id": "1"}, params); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var got cosmosSampleItem
	if err := s.Get(&got, map[string]any{"id": "1"}, params); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := transport.lastQuerySession(t); got != "0:-1#43" {
		t.Fatalf("Get after Delete sent session token %q; want %q", got, "0:-1#43")
	}
}
