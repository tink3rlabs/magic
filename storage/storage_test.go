package storage_test

import (
	"os"
	"testing"

	"github.com/tink3rlabs/magic/storage"
)

func TestGetInstanceReturnsExpectedAdapterForEachType(t *testing.T) {
	tests := []struct {
		name   string
		typ    storage.StorageAdapterType
		config map[string]string
	}{
		{"CASSANDRA", storage.CASSANDRA,
			getCassandraHostAndKeyspace(),
		},
		{"COSMOSDB", storage.COSMOSDB,
			getCosmosDBConfig(),
		},
		{"DYNAMODB", storage.DYNAMODB,
			map[string]string{
				"region":   "us-west-2",
				"endpoint": "http://localhost:8000",
			},
		},
		{"MEMORY", storage.MEMORY, nil},
		{"SQL", storage.SQL, map[string]string{"dsn": "test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := storage.StorageAdapterFactory{}.GetInstance(tt.typ, tt.config)
			if err != nil {
				t.Fatalf("GetInstance(%s) returned error: %v", tt.name, err)
			}
			if adapter == nil {
				t.Fatalf("GetInstance(%s) returned nil adapter", tt.name)
			}
			if adapter.GetType() != tt.typ {
				t.Fatalf("adapter.GetType() = %q; want %q", adapter.GetType(), tt.typ)
			}
		})
	}
}

func getCassandraHostAndKeyspace() map[string]string {
	host := os.Getenv("CASSANDRA_HOSTS")
	if host == "" {
		// Default for local DevContainer
		host = "host.docker.internal"
	}
	keyspace := os.Getenv("CASSANDRA_KEYSPACE")
	if keyspace == "" {
		keyspace = "testkeyspace"
	}
	return map[string]string{
		"hosts":    host,
		"keyspace": keyspace,
	}
}

func getCosmosDBConfig() map[string]string {
	endpoint := os.Getenv("COSMOSDB_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://localhost:8081/"
	}
	key := os.Getenv("COSMOSDB_KEY")
	if key == "" {
		key = "C2y6yDjf5/R+ob0N8A7Cgv30VRDJIWEHLM+4QDU5DE2nQ9nDuVTqobD4b8mGGg=="
	}
	database := os.Getenv("COSMOSDB_DATABASE")
	if database == "" {
		database = "magic"
	}
	return map[string]string{
		"endpoint": endpoint,
		"key":      key,
		"database": database,
	}
}
