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
	Create(item any) error
	Get(dest any, filter map[string]any) error
	Update(item any, filter map[string]any) error
	Delete(item any, filter map[string]any) error
	List(dest any, sortKey string, filter map[string]any, limit int, cursor string) (string, error)
	Search(dest any, sortKey string, query string, limit int, cursor string) (string, error)
	Count(dest any, filter map[string]any) (int64, error)
	Query(dest any, statement string, limit int, cursor string) (string, error)
}

type StorageAdapterType string
type StorageProviders string
type StorageAdapterFactory struct{}

const (
	MEMORY   StorageAdapterType = "memory"
	SQL      StorageAdapterType = "sql"
	DYNAMODB StorageAdapterType = "dynamodb"
)

const (
	POSTGRESQL StorageProviders = "postgresql"
	MYSQL      StorageProviders = "mysql"
	SQLITE     StorageProviders = "sqlite"
)

func (s StorageAdapterFactory) GetInstance(adapterType StorageAdapterType, config any) (StorageAdapter, error) {
	if config == nil {
		config = make(map[string]string)
	}
	switch adapterType {
	case MEMORY:
		return GetMemoryAdapterInstance(), nil
	case SQL:
		return GetSQLAdapterInstance(config.(map[string]string)), nil
	case DYNAMODB:
		return GetDynamoDBAdapterInstance(config.(map[string]string)), nil
	default:
		return nil, errors.New("this storage adapter type isn't supported")
	}
}
