package storage

import (
	"embed"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"
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
	Create(item any, params ...map[string]any) error
	Get(dest any, filter map[string]any, params ...map[string]any) error
	Update(item any, filter map[string]any, params ...map[string]any) error
	Delete(item any, filter map[string]any, params ...map[string]any) error
	// List returns a page of items matching filter, ordered by sortKey.
	// sortKey must be the JSON/column name (e.g. "created_at"), not the Go struct field name.
	// Pass SortDirectionKey via params to control order; defaults to Ascending.
	// Returns a cursor for the next page, or "" on the final page.
	List(dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error)
	// Search returns a page of items matching a Lucene query string, ordered by sortKey.
	// sortKey must be the JSON/column name (e.g. "created_at"), not the Go struct field name.
	// Pass SortDirectionKey via params to control order; defaults to Ascending.
	// Returns a cursor for the next page, or "" on the final page.
	Search(dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error)
	Count(dest any, filter map[string]any, params ...map[string]any) (int64, error)
	Query(dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error)
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

const SortDirectionKey = "sort_direction"

// extractParams merges all provided parameter maps into a single flat map.
// When keys collide, later maps win.
func extractParams(params ...map[string]any) map[string]any {
	flatParams := make(map[string]any)
	for _, param := range params {
		maps.Copy(flatParams, param)
	}
	return flatParams
}

// extractSortDirection reads SortDirectionKey from paramMap and returns the corresponding SortingDirection.
// Defaults to Ascending when the key is absent.
// Returns an error if the value is present but not a valid SortingDirection ("ASC" or "DESC", case-insensitive).
func extractSortDirection(paramMap map[string]any) (SortingDirection, error) {
	if dir, exists := paramMap[SortDirectionKey]; exists {
		if dirStr, ok := dir.(string); ok {
			switch SortingDirection(strings.ToUpper(dirStr)) {
			case Ascending:
				return Ascending, nil
			case Descending:
				return Descending, nil
			}
		}
		return "", fmt.Errorf("invalid sort direction: %v", dir)
	}
	return Ascending, nil
}

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
