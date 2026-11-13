package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tink3rlabs/magic/storage"
)

func TestSQLAdapterGetReportsDriverErrorsNotErrNotFound(t *testing.T) {
	_, sql := setupSQLCoverage(t)

	var got sqlCoverageItem
	err := sql.Get(&got, map[string]any{"no_such_column": "x"})
	if err == nil {
		t.Fatal("Get with unknown column: want error, got nil")
	}
	if errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get with unknown column returned ErrNotFound; want driver error, got %v", err)
	}
}

func TestSQLAdapterGetReportsCanceledContext(t *testing.T) {
	_, sql := setupSQLCoverage(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var got sqlCoverageItem
	err := sql.GetContext(ctx, &got, map[string]any{"id": "x"})
	if err == nil {
		t.Fatal("GetContext with canceled ctx: want error, got nil")
	}
	if errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("canceled Get returned ErrNotFound; want context error, got %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}
