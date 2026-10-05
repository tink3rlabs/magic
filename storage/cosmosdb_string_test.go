package storage

import (
	"context"
	"strings"
	"testing"
)

type cosmosStringSample struct {
	ID string `json:"id"`
	PK string `json:"pk"`
}

func TestNonEmptyString(t *testing.T) {
	if _, err := nonEmptyString("ok", "item id"); err != nil {
		t.Fatalf("string: %v", err)
	}
	for _, tc := range []struct {
		name string
		v    any
	}{
		{"nil", nil},
		{"int", 42},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := nonEmptyString(tc.v, "item id")
			if err == nil || !strings.Contains(err.Error(), "non-empty string") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestCosmosDBCreateContextRejectsNonStringID(t *testing.T) {
	s := &CosmosDBAdapter{}
	// itemToMap of a struct with non-string id isn't easy; pass a map via pointer
	// by using a typed item whose JSON id is set through reflection map path.
	// CreateContext reads itemToMap; use a map-shaped workaround via struct with any?
	type badID struct {
		ID any    `json:"id"`
		PK string `json:"pk"`
	}
	err := s.CreateContext(context.Background(), &badID{ID: 42, PK: "p"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "non-empty string") {
		t.Fatalf("error %q", err.Error())
	}
}

func TestCosmosDBCreateContextRejectsMissingID(t *testing.T) {
	s := &CosmosDBAdapter{}
	type noID struct {
		PK string `json:"pk"`
	}
	err := s.CreateContext(context.Background(), &noID{PK: "p"}, nil)
	if err == nil || !strings.Contains(err.Error(), "id field") {
		t.Fatalf("got %v", err)
	}
}
