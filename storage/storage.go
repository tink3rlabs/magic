package storage

import (
	"embed"
	"errors"
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
	List(dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error)
	Search(dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error)
	Count(dest any, filter map[string]any, params ...map[string]any) (int64, error)
	Query(dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error)
}

type StorageAdapterType string
type StorageProviders string
type StorageAdapterFactory struct{}

const (
	CASSANDRA StorageAdapterType = "cassandra"
	COSMOSDB  StorageAdapterType = "cosmosdb"
	DYNAMODB  StorageAdapterType = "dynamodb"
	MEMORY    StorageAdapterType = "memory"
	SQL       StorageAdapterType = "sql"
)

const (
	POSTGRESQL        StorageProviders = "postgresql"
	MYSQL             StorageProviders = "mysql"
	SQLITE            StorageProviders = "sqlite"
	COSMOSDB_PROVIDER StorageProviders = "cosmosdb"
)

func (s StorageAdapterFactory) GetInstance(adapterType StorageAdapterType, config any) (StorageAdapter, error) {
	if config == nil {
		config = make(map[string]string)
	}
	switch adapterType {
	case CASSANDRA:
		return GetCassandraAdapter(config.(map[string]string))
	case MEMORY:
		return GetMemoryAdapterInstance(), nil
	case SQL:
		return GetSQLAdapterInstance(config.(map[string]string)), nil
	case DYNAMODB:
		return GetDynamoDBAdapterInstance(config.(map[string]string)), nil
	case COSMOSDB:
		return GetCosmosDBAdapterInstance(config.(map[string]string)), nil
	default:
		return nil, errors.New("this storage adapter type isn't supported")
	}
}
