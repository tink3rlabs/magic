package storage

import (
	"embed"
	"errors"

	"github.com/spf13/viper"
)

var ConfigFs embed.FS
var ErrNotFound = errors.New("the requested resource was not found")

type StorageAdapter interface {
	Execute(statement string) error
	Ping() error
	Create(item any) error
	Get(dest any, itemKey string, itemValue string) error
	Update(item any, itemKey string, itemValue string) error
	Delete(item any, itemKey string, itemValue string) error
	List(items any, itemKey string, limit int, cursor string) (string, error)
}

type StorageAdapterType string
type StorageProviders string
type StorageAdapterFactory struct{}

const (
	DEFAULT  StorageAdapterType = "default"
	MEMORY   StorageAdapterType = "memory"
	SQL      StorageAdapterType = "sql"
	DYNAMODB StorageAdapterType = "dynamodb"
)

const (
	POSTGRESQL StorageProviders = "postgresql"
	MYSQL      StorageProviders = "mysql"
	SQLITE     StorageProviders = "sqlite"
)

func (s StorageAdapterFactory) GetInstance(adapterType StorageAdapterType) (StorageAdapter, error) {
	if adapterType == DEFAULT {
		adapterType = StorageAdapterType(viper.GetString("storage.type"))
	}
	switch adapterType {
	case MEMORY:
		return GetMemoryAdapterInstance(), nil
	case SQL:
		return GetSQLAdapterInstance(), nil
	case DYNAMODB:
		return GetDynamoDBAdapterInstance(), nil
	default:
		return nil, errors.New("this storage adapter type isn't supported")
	}
}
