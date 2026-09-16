package storage

import (
	"context"
	"strings"
	"testing"
)

// Every adapter must satisfy both interfaces. Only MemoryAdapter asserted this
// before, so a signature that drifted on any of the other three was caught
// only indirectly, by whichever caller happened to fail to compile.
var (
	_ StorageAdapter           = (*SQLAdapter)(nil)
	_ StorageAdapter           = (*MemoryAdapter)(nil)
	_ StorageAdapter           = (*DynamoDBAdapter)(nil)
	_ StorageAdapter           = (*CosmosDBAdapter)(nil)
	_ ContextualStorageAdapter = (*SQLAdapter)(nil)
	_ ContextualStorageAdapter = (*MemoryAdapter)(nil)
	_ ContextualStorageAdapter = (*DynamoDBAdapter)(nil)
	_ ContextualStorageAdapter = (*CosmosDBAdapter)(nil)
)

// TestCosmosDBRejectsBadOptionsBeforeAnyIO covers the CosmosDB adapter without
// an emulator. Every operation resolves its options before it touches the
// database client, so a zero-value adapter -- whose clients are nil and would
// panic on use -- is enough to prove that an invalid option is rejected rather
// than carried into a request. If one of these ever panics instead of
// returning an error, that operation has started doing I/O before validating.
func TestCosmosDBRejectsBadOptionsBeforeAnyIO(t *testing.T) {
	ctx := context.Background()
	bad := WithSortDirection(SortingDirection("sideways"))
	filter := map[string]any{"id": "x"}

	cases := map[string]func(*CosmosDBAdapter) error{
		"Create": func(a *CosmosDBAdapter) error { return a.CreateContext(ctx, &cosmosProbe{}, bad) },
		"Get": func(a *CosmosDBAdapter) error {
			return a.GetContext(ctx, &cosmosProbe{}, filter, bad)
		},
		"Update": func(a *CosmosDBAdapter) error {
			return a.UpdateContext(ctx, &cosmosProbe{}, filter, bad)
		},
		"Delete": func(a *CosmosDBAdapter) error {
			return a.DeleteContext(ctx, &cosmosProbe{}, filter, bad)
		},
		"List": func(a *CosmosDBAdapter) error {
			_, err := a.ListContext(ctx, &[]cosmosProbe{}, "id", filter, 10, "", bad)
			return err
		},
		"Search": func(a *CosmosDBAdapter) error {
			_, err := a.SearchContext(ctx, &[]cosmosProbe{}, "id", "", 10, "", bad)
			return err
		},
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked instead of rejecting the option: %v", name, r)
				}
			}()
			err := call(&CosmosDBAdapter{})
			if err == nil {
				t.Fatalf("%s = nil error; want the invalid sort direction rejected", name)
			}
			if !strings.Contains(err.Error(), "invalid sort direction") {
				t.Fatalf("%s error = %q; want it to name the invalid sort direction", name, err)
			}
		})
	}
}

// TestDynamoDBRejectsBadOptionsBeforeAnyIO is the same guard for DynamoDB. It
// needs no endpoint: List and Search validate before building a statement.
func TestDynamoDBRejectsBadOptionsBeforeAnyIO(t *testing.T) {
	ctx := context.Background()
	bad := WithSortDirection(SortingDirection("sideways"))
	adapter := &DynamoDBAdapter{}

	if _, err := adapter.ListContext(ctx, &[]cosmosProbe{}, "id", map[string]any{"id": "x"}, 10, "", bad); err == nil ||
		!strings.Contains(err.Error(), "invalid sort direction") {
		t.Fatalf("ListContext error = %v; want the invalid sort direction rejected", err)
	}
}

type cosmosProbe struct {
	Id string `json:"id"`
}
