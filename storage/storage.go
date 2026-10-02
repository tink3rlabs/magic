package storage

import (
	"embed"
	"errors"
	"fmt"
	"regexp"
)

var ConfigFs embed.FS
var ErrNotFound = errors.New("the requested resource was not found")

type StorageAdapter interface {
	Execute(statement string) error
	Ping() error
	GetType() StorageAdapterType
	GetProvider() StorageProviders
	GetSchemaName() string
	CreateSchema() error
	CreateMigrationTable() error
	UpdateMigrationTable(id int, name string, desc string) error
	GetLatestMigration() (int, error)
	Create(item any, opts ...Option) error
	Get(dest any, filter map[string]any, opts ...Option) error
	// Update writes item to the row its primary key identifies, and only if that
	// row also matches filter. It never creates a row; use Create.
	// By default every field is written. Pass WithFields to write only the fields
	// that changed, which is what keeps two concurrent partial updates to
	// different fields from reverting one another.
	// Returns ErrNotFound when no row matched.
	Update(item any, filter map[string]any, opts ...Option) error
	Delete(item any, filter map[string]any, opts ...Option) error
	// List returns a page of items matching filter, ordered by sortKey.
	// sortKey must be the JSON/column name (e.g. "created_at"), not the Go struct field name.
	// Pass WithSortDirection to control order; defaults to Ascending.
	// Returns a cursor for the next page, or "" on the final page.
	List(dest any, sortKey string, filter map[string]any, limit int, cursor string, opts ...Option) (string, error)
	// Search returns a page of items matching a Lucene query string, ordered by sortKey.
	// sortKey must be the JSON/column name (e.g. "created_at"), not the Go struct field name.
	// Pass WithSortDirection to control order; defaults to Ascending.
	// Returns a cursor for the next page, or "" on the final page.
	Search(dest any, sortKey string, query string, limit int, cursor string, opts ...Option) (string, error)
	Count(dest any, filter map[string]any, opts ...Option) (int64, error)
	Query(dest any, statement string, limit int, cursor string, opts ...Option) (string, error)
}

type StorageAdapterType string
type StorageProviders string
type StorageAdapterFactory struct{}

const (
	// CASSANDRA StorageAdapterType = "cassandra"
	COSMOSDB StorageAdapterType = "cosmosdb"
	DYNAMODB StorageAdapterType = "dynamodb"
	MEMORY   StorageAdapterType = "memory"
	SQL      StorageAdapterType = "sql"
)

const (
	POSTGRESQL        StorageProviders = "postgresql"
	MYSQL             StorageProviders = "mysql"
	SQLITE            StorageProviders = "sqlite"
	COSMOSDB_PROVIDER StorageProviders = "cosmosdb"
)

type SortingDirection string

const (
	Ascending  SortingDirection = "ASC"
	Descending SortingDirection = "DESC"
)

// validColumnName matches identifiers safe to interpolate as SQL/NoSQL column names.
// Allows letters, digits, and underscores; may start with a letter or underscore.
var validColumnName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// validateSortKey returns an error if key contains characters that could enable
// injection via ORDER BY or similar clauses where parameterization is not available.
func validateSortKey(key string) error {
	if !validColumnName.MatchString(key) {
		return fmt.Errorf("invalid sort key %q: must match [a-zA-Z_][a-zA-Z0-9_]*", key)
	}
	return nil
}

// GetInstance constructs the requested storage adapter and wraps it with the
// telemetry instrumented adapter from wrapForTelemetry. Callers that need the
// concrete implementation (for example *SQLAdapter to register a GORM plugin) must
// use UnwrapAdapter first (see also TelemetryUnwrapper); otherwise type
// assertions to *SQLAdapter fail when the wrapper sits in front.
func (s StorageAdapterFactory) GetInstance(adapterType StorageAdapterType, config any) (StorageAdapter, error) {
	if config == nil {
		config = make(map[string]string)
	}
	var (
		inner StorageAdapter
		err   error
	)
	switch adapterType {
	// case CASSANDRA:
	// 	return GetCassandraAdapter(config.(map[string]string))
	case MEMORY:
		inner = GetMemoryAdapterInstance()
	case SQL:
		inner = GetSQLAdapterInstance(config.(map[string]string))
	case DYNAMODB:
		inner = GetDynamoDBAdapterInstance(config.(map[string]string))
	case COSMOSDB:
		inner = GetCosmosDBAdapterInstance(config.(map[string]string))
	default:
		err = errors.New("this storage adapter type isn't supported")
	}
	if err != nil {
		return nil, err
	}
	// Wrap with the telemetry-aware adapter unconditionally. When
	// observability has not been initialized the global telemetry
	// is the no-op backend, so the wrapper adds negligible overhead
	// and still produces ContextualStorageAdapter-compatible
	// methods for callers that want to propagate a context.
	return wrapForTelemetry(inner), nil
}
