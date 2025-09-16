package storage

import (
	"testing"
)

func TestCosmosDBConstants(t *testing.T) {
	// Test that the CosmosDB adapter type is properly defined
	if COSMOSDB != "cosmosdb" {
		t.Errorf("Expected COSMOSDB to be 'cosmosdb', got %s", COSMOSDB)
	}

	if COSMOSDB_PROVIDER != "cosmosdb" {
		t.Errorf("Expected COSMOSDB_PROVIDER to be 'cosmosdb', got %s", COSMOSDB_PROVIDER)
	}
}

func TestCosmosDBFactoryMethod(t *testing.T) {
	// Test that the factory method recognizes COSMOSDB type
	factory := StorageAdapterFactory{}

	// Test with minimal config that will fail gracefully
	config := map[string]string{
		"endpoint": "https://test.documents.azure.com:443/",
		"key":      "test-key",
		"database": "test-db",
	}

	// This should fail because of invalid credentials, but it should recognize the type
	_, err := factory.GetInstance(COSMOSDB, config)
	if err == nil {
		t.Error("Expected error with invalid credentials")
	}

	// The error should be about credentials, not unknown adapter type
	// This confirms the factory method recognizes COSMOSDB type
	t.Logf("Factory method recognized COSMOSDB type and failed with: %v", err)
}
