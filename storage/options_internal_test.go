package storage

import "testing"

// These exercise ResolveOptions directly. Going through an adapter would not pin
// them: the SQL adapter also looks every field up in the item's schema, so a
// rejected name there proves only that the schema lookup ran, not that the
// option set validated anything. An adapter without a schema to consult has
// nothing but this validation standing between a caller and an injected
// identifier.

func TestResolveOptionsDefaultsToAscending(t *testing.T) {
	got, err := ResolveOptions()
	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	if got.SortDirection != Ascending {
		t.Fatalf("SortDirection = %q; want %q", got.SortDirection, Ascending)
	}
	if got.FieldsSet {
		t.Fatal("FieldsSet = true; want false when WithFields was not passed")
	}
}

func TestResolveOptionsRejectsUnknownSortDirection(t *testing.T) {
	if _, err := ResolveOptions(WithSortDirection(SortingDirection("sideways"))); err == nil {
		t.Fatal("ResolveOptions = nil error; want an unknown sort direction rejected")
	}
}

func TestResolveOptionsRejectsUnsafeFieldName(t *testing.T) {
	for _, field := range []string{
		"name, color",
		"name;DROP TABLE t",
		"name)",
		"1name",
		"",
		"na me",
	} {
		if _, err := ResolveOptions(WithFields(field)); err == nil {
			t.Fatalf("ResolveOptions(WithFields(%q)) = nil error; want it rejected", field)
		}
	}
}

func TestResolveOptionsAcceptsOrdinaryFieldNames(t *testing.T) {
	got, err := ResolveOptions(WithFields("name", "modified_at", "_internal", "col2"))
	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	if len(got.Fields) != 4 || !got.FieldsSet {
		t.Fatalf("Fields = %v (set=%v); want all four recorded", got.Fields, got.FieldsSet)
	}
}

func TestResolveOptionsRejectsEmptyFieldList(t *testing.T) {
	if _, err := ResolveOptions(WithFields()); err == nil {
		t.Fatal("ResolveOptions(WithFields()) = nil error; want an empty field list rejected")
	}
}

func TestResolveOptionsLaterOptionWins(t *testing.T) {
	got, err := ResolveOptions(WithSortDirection(Descending), WithSortDirection(Ascending))
	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	if got.SortDirection != Ascending {
		t.Fatalf("SortDirection = %q; want the later option to win", got.SortDirection)
	}
}

func TestResolveOptionsIgnoresNilOption(t *testing.T) {
	if _, err := ResolveOptions(nil, WithFields("name")); err != nil {
		t.Fatalf("ResolveOptions with a nil option: %v", err)
	}
}

func TestResolveOptionsRejectsPartitionKeyValueWithoutField(t *testing.T) {
	if _, err := ResolveOptions(WithPartitionKey("", "v")); err == nil {
		t.Fatal("ResolveOptions = nil error; want a partition key value without a field rejected")
	}
}
