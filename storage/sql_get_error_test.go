package storage_test

import (
	"errors"
	"strings"
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
	if !strings.Contains(strings.ToLower(err.Error()), "no_such_column") &&
		!strings.Contains(strings.ToLower(err.Error()), "no such column") &&
		!strings.Contains(strings.ToLower(err.Error()), "has no column") {
		// SQLite wording varies; accept any non-ErrNotFound driver failure.
		t.Logf("driver error (ok if not ErrNotFound): %v", err)
	}
}
