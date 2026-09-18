package storage

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// These run against a real DynamoDB API -- DynamoDB Local, LocalStack, or AWS
// itself -- reached through DYNAMODB_TEST_ENDPOINT. Without that variable there
// is nothing to talk to and they skip. CI sets it, so the skip is a local
// convenience and never a silent pass.
//
// Point them at anything speaking the DynamoDB API. DynamoDB Local is the
// lightest, and is what CI runs:
//
//	docker run -d --rm -p 8000:8000 amazon/dynamodb-local:2.5.2
//	DYNAMODB_TEST_ENDPOINT=http://localhost:8000 go test ./storage/ -run DynamoDB
//
// LocalStack works too, as long as dynamodb is among its enabled services:
//
//	docker run -d --rm -p 4566:4566 -e SERVICES=dynamodb localstack/localstack
//	DYNAMODB_TEST_ENDPOINT=http://localhost:4566 go test ./storage/ -run DynamoDB
//
// Both were used to check this file in.
//
// The table name the adapter derives from the item type is dynamo_items.

// waitForTable bounds how long CreateTable is given to settle. DynamoDB Local
// and LocalStack are near-instant; real DynamoDB takes a few seconds.
const waitForTable = 30 * time.Second

type dynamoItem struct {
	Id    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func newDynamoTestAdapter(t *testing.T) *DynamoDBAdapter {
	t.Helper()
	endpoint := os.Getenv("DYNAMODB_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("DYNAMODB_TEST_ENDPOINT is not set; skipping DynamoDB integration tests")
	}

	// Deliberately not GetDynamoDBAdapterInstance: that caches a process-wide
	// singleton, so the first config to reach it would decide every later test's
	// endpoint.
	adapter := &DynamoDBAdapter{config: map[string]string{
		"endpoint":   endpoint,
		"region":     "us-east-1",
		"access_key": "test",
		"secret_key": "test",
	}}
	adapter.OpenConnection()

	if err := adapter.PingContext(context.Background()); err != nil {
		t.Fatalf("DynamoDB at %s is not reachable: %v", endpoint, err)
	}
	resetDynamoTable(t, adapter)
	return adapter
}

func resetDynamoTable(t *testing.T, adapter *DynamoDBAdapter) {
	t.Helper()
	ctx := context.Background()
	const table = "dynamo_items"

	_, err := adapter.DB.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(table)})
	if err != nil {
		var notFound *types.ResourceNotFoundException
		if !errors.As(err, &notFound) {
			t.Fatalf("DeleteTable: %v", err)
		}
	}

	_, err = adapter.DB.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   aws.String(table),
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("id"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("id"), KeyType: types.KeyTypeHash},
		},
	})
	if err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if err := dynamodb.NewTableExistsWaiter(adapter.DB).Wait(ctx,
		&dynamodb.DescribeTableInput{TableName: aws.String(table)}, waitForTable); err != nil {
		t.Fatalf("waiting for table: %v", err)
	}
}

func TestDynamoDBCreateAndGet(t *testing.T) {
	adapter := newDynamoTestAdapter(t)

	if err := adapter.Create(&dynamoItem{Id: "d1", Name: "original", Color: "red"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	var got dynamoItem
	if err := adapter.Get(&got, map[string]any{"id": "d1"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "original" || got.Color != "red" {
		t.Fatalf("got %+v; want name=original color=red", got)
	}
}

// TestDynamoDBUpdateWithoutFieldsWritesEveryAttribute pins the default, and it
// is the same default the SQL adapter has: with no WithFields, every attribute
// of the item is written, so a field left zero in the struct overwrites what
// was stored. That is a caller passing an incomplete item, not the adapter
// losing data -- the distinction matters, because before #263 the same call
// *deleted* the attribute rather than setting it.
func TestDynamoDBUpdateWithoutFieldsWritesEveryAttribute(t *testing.T) {
	adapter := newDynamoTestAdapter(t)

	if err := adapter.Create(&dynamoItem{Id: "d1", Name: "original", Color: "red"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := adapter.Update(&dynamoItem{Id: "d1", Name: "renamed"}, map[string]any{"id": "d1"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var got dynamoItem
	if err := adapter.Get(&got, map[string]any{"id": "d1"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "renamed" {
		t.Fatalf("name = %q; want %q", got.Name, "renamed")
	}
	if got.Color != "" {
		t.Fatalf("color = %q; want the zero value written, as Select(\"*\") does on SQL", got.Color)
	}
}

// TestDynamoDBUpdateRespectsItsFilter is the contract #258 gave the SQL
// adapter: write only if the row also matches the filter, and report
// ErrNotFound when it does not. Update used to forward to a PutItem without
// the filter, so a filter matching nothing still wrote.
func TestDynamoDBUpdateRespectsItsFilter(t *testing.T) {
	adapter := newDynamoTestAdapter(t)

	if err := adapter.Create(&dynamoItem{Id: "d1", Name: "original", Color: "red"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := adapter.Update(
		&dynamoItem{Id: "d1", Name: "overwritten", Color: "blue"},
		map[string]any{"id": "d1", "name": "a value the row does not have"},
	)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update with a non-matching filter = %v; want ErrNotFound", err)
	}

	var got dynamoItem
	if err := adapter.Get(&got, map[string]any{"id": "d1"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "original" || got.Color != "red" {
		t.Fatalf("got %+v; want the row untouched", got)
	}
}

// TestDynamoDBUpdateDoesNotCreateMissingItem matches the SQL adapter: Update
// never creates. UpdateItem will happily create the item when the key is
// absent, so this needs an explicit condition on the key.
func TestDynamoDBUpdateDoesNotCreateMissingItem(t *testing.T) {
	adapter := newDynamoTestAdapter(t)

	err := adapter.Update(&dynamoItem{Id: "ghost", Name: "n"}, map[string]any{"id": "ghost"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update of a missing item = %v; want ErrNotFound", err)
	}
	var got dynamoItem
	if err := adapter.Get(&got, map[string]any{"id": "ghost"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v; want the item never created", err)
	}
}

// TestDynamoDBUpdateHonoursWithFields is the point of the exercise: write only
// the named attributes and leave the rest of the stored item alone, which
// DynamoDB expresses natively with an UpdateExpression.
func TestDynamoDBUpdateHonoursWithFields(t *testing.T) {
	adapter := newDynamoTestAdapter(t)

	if err := adapter.Create(&dynamoItem{Id: "d1", Name: "original", Color: "red"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := adapter.Update(&dynamoItem{Id: "d1", Name: "renamed"},
		map[string]any{"id": "d1"}, WithFields("name")); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var got dynamoItem
	if err := adapter.Get(&got, map[string]any{"id": "d1"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "renamed" || got.Color != "red" {
		t.Fatalf("got %+v; want name=renamed color=red", got)
	}
}

// TestDynamoDBListWithoutFilterRejectsOrdering covers what the service itself
// refuses. PartiQL will not accept ORDER BY without a WHERE, and before this
// the adapter appended ORDER BY regardless, so every unfiltered List with a
// sort key came back as an opaque ValidationException from inside
// ExecuteStatement. Fail with something that names the cause instead.
func TestDynamoDBListWithoutFilterRejectsOrdering(t *testing.T) {
	adapter := newDynamoTestAdapter(t)

	var page []dynamoItem
	_, err := adapter.List(&page, "id", nil, 10, "", WithSortDirection(Descending))
	if err == nil {
		t.Fatal("List = nil error; want an unfiltered ordered scan rejected")
	}
	if !strings.Contains(err.Error(), "requires a filter") {
		t.Fatalf("error %q; want it to name the missing filter rather than surface a ValidationException", err)
	}
}

// TestDynamoDBListCannotOptOutOfOrdering is why the test above matters more
// than it looks. The adapter guards ORDER BY with `if sortKey != ""`, but
// validateSortKey rejects an empty sort key before that branch is ever
// reached, so the branch is dead and a caller cannot ask for an unordered
// scan. Combined with PartiQL refusing ORDER BY without a WHERE, that makes an
// unfiltered List on this adapter not expressible at all -- it is not merely
// the ordered case that fails.
//
// Pinning it here so that if validateSortKey ever learns to allow "", whoever
// makes that change sees this and can let an unfiltered scan through.
func TestDynamoDBListCannotOptOutOfOrdering(t *testing.T) {
	adapter := newDynamoTestAdapter(t)

	var page []dynamoItem
	_, err := adapter.List(&page, "", nil, 10, "")
	if err == nil {
		t.Fatal("List with an empty sort key = nil error; the dead ORDER BY branch is now reachable")
	}
	if !strings.Contains(err.Error(), "invalid sort key") {
		t.Fatalf("error %q; want the empty sort key rejected by validateSortKey", err)
	}
}

// dynamoRanged has a composite key so that several items share one partition.
// dynamoItem cannot serve here: its only key is the hash key, so a filter that
// scopes to a partition also narrows to a single item and the ordering of the
// result is unobservable. A direction test against it would pass whether or not
// the direction was emitted at all.
type dynamoRanged struct {
	Tenant string `json:"tenant"`
	Id     string `json:"id"`
}

func resetDynamoRangedTable(t *testing.T, adapter *DynamoDBAdapter) {
	t.Helper()
	ctx := context.Background()
	const table = "dynamo_rangeds"

	if _, err := adapter.DB.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(table)}); err != nil {
		var notFound *types.ResourceNotFoundException
		if !errors.As(err, &notFound) {
			t.Fatalf("DeleteTable: %v", err)
		}
	}
	if _, err := adapter.DB.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   aws.String(table),
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("tenant"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("id"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("tenant"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("id"), KeyType: types.KeyTypeRange},
		},
	}); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if err := dynamodb.NewTableExistsWaiter(adapter.DB).Wait(ctx,
		&dynamodb.DescribeTableInput{TableName: aws.String(table)}, waitForTable); err != nil {
		t.Fatalf("waiting for table: %v", err)
	}
}

// TestDynamoDBListEmitsSortDirection pins that the direction reaches the
// statement. Before options were resolved here the adapter emitted a bare
// ORDER BY with no ASC or DESC, so WithSortDirection was accepted and silently
// dropped on this adapter.
func TestDynamoDBListEmitsSortDirection(t *testing.T) {
	adapter := newDynamoTestAdapter(t)
	resetDynamoRangedTable(t, adapter)

	for _, id := range []string{"a", "b", "c"} {
		if err := adapter.Create(&dynamoRanged{Tenant: "t1", Id: id}); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	partition := map[string]any{"tenant": "t1"}

	var desc []dynamoRanged
	if _, err := adapter.List(&desc, "id", partition, 10, "", WithSortDirection(Descending)); err != nil {
		t.Fatalf("descending List: %v", err)
	}
	if got := ids(desc); got != "c,b,a" {
		t.Fatalf("descending order = %s; want c,b,a", got)
	}

	var asc []dynamoRanged
	if _, err := adapter.List(&asc, "id", partition, 10, ""); err != nil {
		t.Fatalf("default List: %v", err)
	}
	if got := ids(asc); got != "a,b,c" {
		t.Fatalf("default order = %s; want a,b,c (ascending)", got)
	}
}

func ids(items []dynamoRanged) string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Id)
	}
	return strings.Join(out, ",")
}

func TestDynamoDBDelete(t *testing.T) {
	adapter := newDynamoTestAdapter(t)

	if err := adapter.Create(&dynamoItem{Id: "d1", Name: "original"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := adapter.Delete(&dynamoItem{}, map[string]any{"id": "d1"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var got dynamoItem
	if err := adapter.Get(&got, map[string]any{"id": "d1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v; want ErrNotFound", err)
	}
}
