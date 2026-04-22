package storage

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// getTableName, buildFilter, and buildParams are pure helpers
// that do no I/O, so we can exercise them against a zero-value
// DynamoDBAdapter without opening an AWS session. They are the
// bulk of the logic third-party callers rely on, so covering
// them meaningfully raises the DynamoDB adapter's testable
// surface without requiring a live DynamoDB instance.

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
	got := s.buildFilter(map[string]any{"id": "42"})
	if got != "id=?" {
		t.Fatalf("buildFilter scalar = %q; want %q", got, "id=?")
	}
}

func TestDynamoDBBuildFilterSlice(t *testing.T) {
	s := &DynamoDBAdapter{}
	got := s.buildFilter(map[string]any{"id": []string{"a", "b", "c"}})
	// Slice values become IN(?,?,?) with no trailing comma.
	if got != "id IN (?,?,?)" {
		t.Fatalf("buildFilter slice = %q; want %q", got, "id IN (?,?,?)")
	}
}

func TestDynamoDBBuildFilterMixedClausesAreJoinedWithAnd(t *testing.T) {
	s := &DynamoDBAdapter{}
	// Map iteration order is random, so we validate structure
	// rather than an exact string.
	got := s.buildFilter(map[string]any{
		"id":     "42",
		"status": []string{"active", "pending"},
	})
	parts := strings.Split(got, " AND ")
	if len(parts) != 2 {
		t.Fatalf("expected 2 AND-joined clauses, got %q", got)
	}
	found := map[string]bool{"id=?": false, "status IN (?,?)": false}
	for _, p := range parts {
		if _, ok := found[p]; ok {
			found[p] = true
		}
	}
	for clause, seen := range found {
		if !seen {
			t.Fatalf("missing expected clause %q in %q", clause, got)
		}
	}
}

func TestDynamoDBBuildParamsScalarAndSlice(t *testing.T) {
	s := &DynamoDBAdapter{}
	params, err := s.buildParams(map[string]any{
		"id":     "42",
		"status": []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("buildParams: %v", err)
	}
	// 1 scalar + 2 slice entries = 3 marshalled attribute values.
	if len(params) != 3 {
		t.Fatalf("len(params) = %d; want 3", len(params))
	}
	for i, p := range params {
		if p == nil {
			t.Fatalf("params[%d] is nil", i)
		}
	}
}

func TestDynamoDBBuildParamsEmptyFilterReturnsEmpty(t *testing.T) {
	s := &DynamoDBAdapter{}
	params, err := s.buildParams(map[string]any{})
	if err != nil {
		t.Fatalf("buildParams: %v", err)
	}
	if len(params) != 0 {
		t.Fatalf("expected 0 params for empty filter, got %d", len(params))
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

func TestDynamoDBBuildParamsMarshalsIntoMemberAttributeValue(t *testing.T) {
	s := &DynamoDBAdapter{}
	params, err := s.buildParams(map[string]any{"n": 7})
	if err != nil {
		t.Fatalf("buildParams: %v", err)
	}
	if len(params) != 1 {
		t.Fatalf("len(params) = %d; want 1", len(params))
	}
	// Integer scalars marshal to AttributeValueMemberN.
	if _, ok := params[0].(*types.AttributeValueMemberN); !ok {
		t.Fatalf("params[0] = %T; want *AttributeValueMemberN", params[0])
	}
}
