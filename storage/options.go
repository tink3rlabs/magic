package storage

import "fmt"

// Option configures a single storage operation.
//
// One option set serves every operation and every adapter, which is what the
// former `params ...map[string]any` provided — but typed, so the compiler
// checks the value and godoc lists what is available. An option an operation
// or adapter has no use for is accepted and ignored; each option's doc comment
// names the operations that read it.
type Option func(*Options)

// Options is the resolved configuration for one storage call. Adapters read it;
// callers build it with the With* functions rather than filling it in directly.
type Options struct {
	// SortDirection orders results. Read by List and Search. Defaults to Ascending.
	SortDirection SortingDirection

	// Fields restricts a write to these fields, named as the storage layer names
	// them — the column name on SQL, the attribute name on DynamoDB. Read by
	// Update. Nil means write every field.
	Fields []string

	// FieldsSet reports whether WithFields was passed at all. It tells an empty
	// field list, which is a caller mistake, apart from no option at all, which
	// means write every field.
	FieldsSet bool

	// PartitionKeyField names the item field holding the partition key.
	// PartitionKeyValue addresses a partition directly instead of reading it off
	// the item. Both are read by the CosmosDB adapter.
	PartitionKeyField string
	PartitionKeyValue any
}

// WithSortDirection orders List and Search results. An unrecognized direction
// is rejected when the options are resolved rather than quietly ignored.
func WithSortDirection(direction SortingDirection) Option {
	return func(o *Options) { o.SortDirection = direction }
}

// WithFields restricts Update to the named fields.
//
// Without it Update writes every field of the item, so two callers that each
// read a record and then change different fields both write back the values
// they read for the fields they never touched, and the second write silently
// reverts the first. Naming the fields that actually changed keeps concurrent
// edits to different fields from colliding.
//
// It also makes clearing a field work: the named fields are written whether or
// not they hold a zero value.
//
// Passing no field at all is an error — an update that writes nothing is never
// what a caller meant, and skipping the call is the caller's decision to make.
func WithFields(fields ...string) Option {
	return func(o *Options) {
		o.Fields = fields
		o.FieldsSet = true
	}
}

// WithPartitionKey addresses a CosmosDB partition by field and value.
func WithPartitionKey(field string, value any) Option {
	return func(o *Options) {
		o.PartitionKeyField = field
		o.PartitionKeyValue = value
	}
}

// WithPartitionKeyField names the item field holding the CosmosDB partition
// key, leaving the adapter to read the value off the item.
func WithPartitionKeyField(field string) Option {
	return func(o *Options) { o.PartitionKeyField = field }
}

// newOptions resolves opts over the defaults and validates the result.
func newOptions(opts ...Option) (Options, error) {
	resolved := Options{SortDirection: Ascending}
	for _, opt := range opts {
		if opt != nil {
			opt(&resolved)
		}
	}
	return resolved, resolved.validate()
}

func (o Options) validate() error {
	switch o.SortDirection {
	case Ascending, Descending:
	default:
		return fmt.Errorf("invalid sort direction %q: want %q or %q", o.SortDirection, Ascending, Descending)
	}
	if o.FieldsSet && len(o.Fields) == 0 {
		return fmt.Errorf("WithFields requires at least one field")
	}
	if o.PartitionKeyValue != nil && o.PartitionKeyField == "" {
		return fmt.Errorf("a partition key value needs a field to go with it: use WithPartitionKey")
	}
	// The field names reach clauses that are not parameterized, so they are
	// checked the same way sort keys are.
	for _, field := range o.Fields {
		if !validColumnName.MatchString(field) {
			return fmt.Errorf("invalid field name %q: must match [a-zA-Z_][a-zA-Z0-9_]*", field)
		}
	}
	return nil
}
