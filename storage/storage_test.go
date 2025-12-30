package storage_test

import (
	"os"
	"testing"

	"github.com/tink3rlabs/magic/storage"
)

func TestGetInstanceReturnsExpectedAdapterForEachType(t *testing.T) {
	host, keyspace := getCassandraHostAndKeyspace()
	tests := []struct {
		name   string
		typ    storage.StorageAdapterType
		config map[string]string
	}{
		{
			"CASSANDRA",
			storage.CASSANDRA,
			map[string]string{
				"hosts":    host,
				"keyspace": keyspace,
			},
		},
		// {"COSMOSDB", storage.COSMOSDB,
		// 	map[string]string{
		// 		"endpoint": "https://your-cosmosdb-account.documents.azure.com:443/",
		// 		"key":      "your-cosmosdb-key",
		// 		"database": "magic",
		// 	},
		// },
		// {"DYNAMODB", storage.DYNAMODB,
		// 	map[string]string{
		// 		"region":   "us-west-2",
		// 		"endpoint": "http://localhost:8000",
		// 	},
		// },
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

func getCassandraHostAndKeyspace() (string, string) {
	host := os.Getenv("CASSANDRA_HOSTS")
	if host == "" {
		// Default for local DevContainer
		host = "host.docker.internal"
	}
	keyspace := os.Getenv("CASSANDRA_KEYSPACE")
	if keyspace == "" {
		keyspace = "testkeyspace"
	}
	return host, keyspace
}
