package storage

import "testing"

// These exercise newOptions directly. Going through an adapter would not pin
// them: the SQL adapter also looks every field up in the item's schema, so a
// rejected name there proves only that the schema lookup ran, not that the
// option set validated anything. An adapter without a schema to consult has
// nothing but this validation standing between a caller and an injected
// identifier.

func TestNewOptionsDefaultsToAscending(t *testing.T) {
	got, err := newOptions()
	if err != nil {
		t.Fatalf("newOptions: %v", err)
	}
	if got.SortDirection != Ascending {
		t.Fatalf("SortDirection = %q; want %q", got.SortDirection, Ascending)
	}
	if got.FieldsSet {
		t.Fatal("FieldsSet = true; want false when WithFields was not passed")
	}
}

func TestNewOptionsRejectsUnknownSortDirection(t *testing.T) {
	if _, err := newOptions(WithSortDirection(SortingDirection("sideways"))); err == nil {
		t.Fatal("newOptions = nil error; want an unknown sort direction rejected")
	}
}

func TestNewOptionsRejectsUnsafeFieldName(t *testing.T) {
	for _, field := range []string{
		"name, color",
		"name;DROP TABLE t",
		"name)",
		"1name",
		"",
		"na me",
	} {
		if _, err := newOptions(WithFields(field)); err == nil {
			t.Fatalf("newOptions(WithFields(%q)) = nil error; want it rejected", field)
		}
	}
}

func TestNewOptionsAcceptsOrdinaryFieldNames(t *testing.T) {
	got, err := newOptions(WithFields("name", "modified_at", "_internal", "col2"))
	if err != nil {
		t.Fatalf("newOptions: %v", err)
	}
	if len(got.Fields) != 4 || !got.FieldsSet {
		t.Fatalf("Fields = %v (set=%v); want all four recorded", got.Fields, got.FieldsSet)
	}
}

func TestNewOptionsRejectsEmptyFieldList(t *testing.T) {
	if _, err := newOptions(WithFields()); err == nil {
		t.Fatal("newOptions(WithFields()) = nil error; want an empty field list rejected")
	}
}

func TestNewOptionsLaterOptionWins(t *testing.T) {
	got, err := newOptions(WithSortDirection(Descending), WithSortDirection(Ascending))
	if err != nil {
		t.Fatalf("newOptions: %v", err)
	}
	if got.SortDirection != Ascending {
		t.Fatalf("SortDirection = %q; want the later option to win", got.SortDirection)
	}
}

func TestNewOptionsIgnoresNilOption(t *testing.T) {
	if _, err := newOptions(nil, WithFields("name")); err != nil {
		t.Fatalf("newOptions with a nil option: %v", err)
	}
}

func TestNewOptionsRejectsPartitionKeyValueWithoutField(t *testing.T) {
	if _, err := newOptions(WithPartitionKey("", "v")); err == nil {
		t.Fatal("newOptions = nil error; want a partition key value without a field rejected")
	}
}
