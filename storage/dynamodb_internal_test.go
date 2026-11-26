package storage

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// getTableName and buildFilter are pure helpers that do no I/O, so we can
// exercise them against a zero-value DynamoDBAdapter without opening an AWS
// session. They are the bulk of the logic third-party callers rely on, so
// covering them meaningfully raises the DynamoDB adapter's testable surface
// without requiring a live DynamoDB instance.

type dynamoSampleItem struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type dynamoTwoWordItem struct {
	Id string `json:"id"`
}

func TestDynamoDBGetTableNameConvertsToSnakeCasePlural(t *testing.T) {
	s := &DynamoDBAdapter{}

	cases := []struct {
		name string
		in   any
		want string
	}{
		{"single-word struct", dynamoSampleItem{}, "dynamo_sample_items"},
		{"pointer receiver", &dynamoSampleItem{}, "dynamo_sample_items"},
		{"two-word type", dynamoTwoWordItem{}, "dynamo_two_word_items"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.getTableName(tc.in); got != tc.want {
				t.Fatalf("getTableName(%T) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDynamoDBBuildFilterScalar(t *testing.T) {
	s := &DynamoDBAdapter{}
	got, values, err := s.buildFilter(map[string]any{"id": "42"})
	if err != nil {
		t.Fatalf("buildFilter: %v", err)
	}
	if got != `"id" = ?` {
		t.Fatalf("buildFilter scalar = %q; want %q", got, `"id" = ?`)
	}
	if len(values) != 1 {
		t.Fatalf("len(values) = %d; want 1", len(values))
	}
}

func TestDynamoDBBuildFilterSlice(t *testing.T) {
	s := &DynamoDBAdapter{}
	got, values, err := s.buildFilter(map[string]any{"id": []string{"a", "b", "c"}})
	if err != nil {
		t.Fatalf("buildFilter: %v", err)
	}
	// Slice values become IN (?, ?, ?) with no trailing comma.
	if got != `"id" IN (?, ?, ?)` {
		t.Fatalf("buildFilter slice = %q; want %q", got, `"id" IN (?, ?, ?)`)
	}
	if len(values) != 3 {
		t.Fatalf("len(values) = %d; want 3", len(values))
	}
}

func TestDynamoDBBuildFilterEmptyFilterReturnsEmpty(t *testing.T) {
	s := &DynamoDBAdapter{}
	got, values, err := s.buildFilter(map[string]any{})
	if err != nil {
		t.Fatalf("buildFilter: %v", err)
	}
	if got != "" || len(values) != 0 {
		t.Fatalf("buildFilter(empty) = %q, %d values; want empty", got, len(values))
	}
}

// PartiQL binds "?" placeholders by position. Building the clause and the
// values in two passes over the map paired them up in random order.
func TestDynamoDBBuildFilterValuesFollowClauseOrder(t *testing.T) {
	s := &DynamoDBAdapter{}
	filter := map[string]any{
		"tenant": "acme",
		"status": []string{"active", "pending"},
		"kind":   "rule",
		"owner":  "bob",
		"region": "eu",
	}
	wantClause := `"kind" = ? AND "owner" = ? AND "region" = ? AND "status" IN (?, ?) AND "tenant" = ?`
	wantValues := []string{"rule", "bob", "eu", "active", "pending", "acme"}

	for range 1000 {
		got, values, err := s.buildFilter(filter)
		if err != nil {
			t.Fatalf("buildFilter: %v", err)
		}
		if got != wantClause {
			t.Fatalf("buildFilter = %q; want %q", got, wantClause)
		}
		if len(values) != len(wantValues) {
			t.Fatalf("len(values) = %d; want %d", len(values), len(wantValues))
		}
		for i, want := range wantValues {
			v, ok := values[i].(*types.AttributeValueMemberS)
			if !ok || v.Value != want {
				t.Fatalf("values[%d] = %#v; want %q", i, values[i], want)
			}
		}
	}
}

func TestDynamoDBBuildFilterReturnsMarshalErrors(t *testing.T) {
	s := &DynamoDBAdapter{}
	unmarshallable := map[[2]int]int{{1, 2}: 1}
	if _, _, err := s.buildFilter(map[string]any{"id": unmarshallable}); err == nil {
		t.Fatal("buildFilter must return an error for a value that can't be marshalled")
	}
}

// Keys are written into the statement, so anything that isn't a plain
// attribute path must be rejected rather than passed through.
func TestDynamoDBBuildFilterRejectsUnsafeKeys(t *testing.T) {
	s := &DynamoDBAdapter{}
	for _, key := range []string{
		"tenant<>'' OR tenant",
		`id" OR "x`,
		"id = ? OR id",
		"a..b",
		".a",
		"a.",
		"",
		"$or",
		"na me",
	} {
		t.Run(key, func(t *testing.T) {
			if got, _, err := s.buildFilter(map[string]any{key: "x"}); err == nil {
				t.Fatalf("buildFilter(%q) = %q; want an error", key, got)
			}
		})
	}
}

func TestDynamoDBBuildFilterQuotesKeys(t *testing.T) {
	s := &DynamoDBAdapter{}
	cases := map[string]string{
		"user-id":      `"user-id" = ?`,
		"status":       `"status" = ?`,
		"name":         `"name" = ?`,
		"address.city": `"address"."city" = ?`,
		"a.b-c.d_e":    `"a"."b-c"."d_e" = ?`,
	}
	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			got, _, err := s.buildFilter(map[string]any{key: "x"})
			if err != nil {
				t.Fatalf("buildFilter(%q): %v", key, err)
			}
			if got != want {
				t.Fatalf("buildFilter(%q) = %q; want %q", key, got, want)
			}
		})
	}
}

func TestDynamoDBBuildFilterNilIsNull(t *testing.T) {
	s := &DynamoDBAdapter{}
	got, values, err := s.buildFilter(map[string]any{"deleted_at": nil, "id": "1"})
	if err != nil {
		t.Fatalf("buildFilter: %v", err)
	}
	if want := `"deleted_at" IS NULL AND "id" = ?`; got != want {
		t.Fatalf("buildFilter = %q; want %q", got, want)
	}
	if len(values) != 1 {
		t.Fatalf("len(values) = %d; want 1 (IS NULL binds nothing)", len(values))
	}
}

func TestDynamoDBBuildFilterEmptySliceMatchesNothing(t *testing.T) {
	s := &DynamoDBAdapter{}
	_, _, err := s.buildFilter(map[string]any{"id": "1", "status": []string{}})
	if !errors.Is(err, errFilterMatchesNothing) {
		t.Fatalf("buildFilter(empty slice) error = %v; want errFilterMatchesNothing", err)
	}
}

func TestDynamoDBListWithEmptySliceReturnsEmptyPageWithoutQuerying(t *testing.T) {
	// A zero-value adapter has no client, so reaching DynamoDB would panic.
	s := &DynamoDBAdapter{}
	dest := []dynamoSampleItem{{Id: "stale"}}
	next, err := s.List(&dest, "id", map[string]any{"id": []string{}}, 10, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if next != "" || len(dest) != 0 {
		t.Fatalf("List = %q, %v; want an empty page", next, dest)
	}
}

func TestDynamoDBBuildFilterBytesAreOneValue(t *testing.T) {
	s := &DynamoDBAdapter{}
	got, values, err := s.buildFilter(map[string]any{"blob": []byte("abc")})
	if err != nil {
		t.Fatalf("buildFilter: %v", err)
	}
	if got != `"blob" = ?` {
		t.Fatalf("buildFilter = %q; want %q", got, `"blob" = ?`)
	}
	if _, ok := values[0].(*types.AttributeValueMemberB); !ok || len(values) != 1 {
		t.Fatalf("values = %#v; want one binary value", values)
	}
}

func TestDynamoDBTrivialGettersAndUnsupportedOps(t *testing.T) {
	s := &DynamoDBAdapter{}

	if got := s.GetType(); got != DYNAMODB {
		t.Fatalf("GetType = %q; want %q", got, DYNAMODB)
	}
	if got := s.GetProvider(); got != "" {
		t.Fatalf("GetProvider = %q; want empty", got)
	}
	if got := s.GetSchemaName(); got != "" {
		t.Fatalf("GetSchemaName = %q; want empty", got)
	}

	// All of the "schema / migrations" entry points on the
	// DynamoDB adapter are documented as unsupported. Pin
	// that so silent regressions (e.g. someone changing a
	// return to nil) fail a test rather than surprise a
	// consumer at runtime.
	if err := s.CreateSchema(); err == nil {
		t.Fatalf("CreateSchema must return an error")
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

func TestDynamoDBBuildFilterMarshalsIntoMemberAttributeValue(t *testing.T) {
	s := &DynamoDBAdapter{}
	_, values, err := s.buildFilter(map[string]any{"n": 7})
	if err != nil {
		t.Fatalf("buildFilter: %v", err)
	}
	if len(values) != 1 {
		t.Fatalf("len(values) = %d; want 1", len(values))
	}
	// Integer scalars marshal to AttributeValueMemberN.
	if _, ok := values[0].(*types.AttributeValueMemberN); !ok {
		t.Fatalf("values[0] = %T; want *AttributeValueMemberN", values[0])
	}
}
