package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/google/uuid"

	"github.com/tink3rlabs/magic/logger"
)

type CosmosDBAdapter struct {
	client   *azcosmos.Client
	database *azcosmos.DatabaseClient
	config   map[string]string
}

var cosmosDBAdapterLock = &sync.Mutex{}
var cosmosDBAdapterInstance *CosmosDBAdapter

func GetCosmosDBAdapterInstance(config map[string]string) *CosmosDBAdapter {
	if cosmosDBAdapterInstance == nil {
		cosmosDBAdapterLock.Lock()
		defer cosmosDBAdapterLock.Unlock()
		if cosmosDBAdapterInstance == nil {
			cosmosDBAdapterInstance = &CosmosDBAdapter{config: config}
			cosmosDBAdapterInstance.OpenConnection()
		}
	}
	return cosmosDBAdapterInstance
}

func (s *CosmosDBAdapter) OpenConnection() {
	// Parse connection string or use individual config values
	var endpoint string
	var key string
	var databaseName string

	if connStr, exists := s.config["connection_string"]; exists {
		// Parse connection string format: AccountEndpoint=https://...;AccountKey=...;
		parts := strings.Split(connStr, ";")
		for _, part := range parts {
			if strings.HasPrefix(part, "AccountEndpoint=") {
				endpoint = strings.TrimPrefix(part, "AccountEndpoint=")
			} else if strings.HasPrefix(part, "AccountKey=") {
				key = strings.TrimPrefix(part, "AccountKey=")
			}
		}
	} else {
		endpoint = s.config["endpoint"]
		key = s.config["key"]
	}

	databaseName = s.config["database"]
	if databaseName == "" {
		databaseName = "magic"
	}

	if endpoint == "" || key == "" {
		logger.Fatal("CosmosDB endpoint and key are required")
	}

	// Create credential
	cred, err := azcosmos.NewKeyCredential(key)
	if err != nil {
		logger.Fatal("failed to create CosmosDB credential", slog.Any("error", err.Error()))
	}

	// Create client options
	clientOptions := azcosmos.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Retry: policy.RetryOptions{
				MaxRetries: 3,
			},
		},
	}

	// Create client
	s.client, err = azcosmos.NewClientWithKey(endpoint, cred, &clientOptions)
	if err != nil {
		logger.Fatal("failed to create CosmosDB client", slog.Any("error", err.Error()))
	}

	// Get database
	s.database, err = s.client.NewDatabase(databaseName)
	if err != nil {
		logger.Fatal("failed to get CosmosDB database", slog.Any("error", err.Error()))
	}

	slog.Debug(fmt.Sprintf("Connected to CosmosDB database: %s", databaseName))
}

func (s *CosmosDBAdapter) Execute(statement string) error {
	// CosmosDB doesn't support arbitrary SQL execution like traditional databases
	// This method is mainly for compatibility with the interface
	return fmt.Errorf("CosmosDB Execute method not supported - use Query method instead")
}

func (s *CosmosDBAdapter) Ping() error {
	// Test connection by trying to read database properties
	_, err := s.database.Read(context.TODO(), &azcosmos.ReadDatabaseOptions{})
	return err
}

func (s *CosmosDBAdapter) GetType() StorageAdapterType {
	return COSMOSDB
}

func (s *CosmosDBAdapter) GetProvider() StorageProviders {
	return COSMOSDB_PROVIDER
}

func (s *CosmosDBAdapter) GetSchemaName() string {
	return s.config["database"]
}

func (s *CosmosDBAdapter) CreateSchema() error {
	// In CosmosDB, databases are created at the account level
	// This method is mainly for compatibility
	return nil
}

func (s *CosmosDBAdapter) CreateMigrationTable() error {
	// CosmosDB doesn't have traditional tables, containers are created dynamically
	// Migration tracking would need to be implemented differently
	return fmt.Errorf("CosmosDB CreateMigrationTable is not supported - migrations should be handled at application level")
}

func (s *CosmosDBAdapter) UpdateMigrationTable(id int, name string, desc string) error {
	return fmt.Errorf("CosmosDB UpdateMigrationTable is not supported")
}

func (s *CosmosDBAdapter) GetLatestMigration() (int, error) {
	return -1, fmt.Errorf("CosmosDB GetLatestMigration is not supported")
}

func (s *CosmosDBAdapter) Create(item any) error {
	containerName := s.getContainerName(item)
	container, err := s.client.NewContainer(s.config["database"], containerName)
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}

	// Add CosmosDB required fields if not present
	itemMap := s.itemToMap(item)
	if _, exists := itemMap["id"]; !exists {
		itemMap["id"] = uuid.New().String()
	}
	if _, exists := itemMap["_ts"]; !exists {
		itemMap["_ts"] = time.Now().Unix()
	}

	// Convert back to the original type
	item = s.mapToItem(itemMap, reflect.TypeOf(item))

	// Marshal item to JSON
	itemBytes, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal item: %v", err)
	}

	_, err = container.CreateItem(context.TODO(), azcosmos.NewPartitionKeyString(""), itemBytes, nil)
	if err != nil {
		return fmt.Errorf("failed to create item: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) Get(dest any, filter map[string]any) error {
	if len(filter) == 0 {
		return fmt.Errorf("filtering is required when getting a resource")
	}

	containerName := s.getContainerName(dest)
	container, err := s.client.NewContainer(s.config["database"], containerName)
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}

	// Build query
	query := "SELECT * FROM c WHERE "
	conditions := []string{}
	for key := range filter {
		conditions = append(conditions, fmt.Sprintf("c.%s = @%s", key, key))
	}
	query += strings.Join(conditions, " AND ")

	// Execute query
	queryOptions := azcosmos.QueryOptions{
		QueryParameters: []azcosmos.QueryParameter{},
	}

	for key, value := range filter {
		queryOptions.QueryParameters = append(queryOptions.QueryParameters, azcosmos.QueryParameter{
			Name:  fmt.Sprintf("@%s", key),
			Value: value,
		})
	}

	pager := container.NewQueryItemsPager(query, azcosmos.NewPartitionKeyString(""), &queryOptions)

	if pager.More() {
		response, err := pager.NextPage(context.TODO())
		if err != nil {
			return fmt.Errorf("failed to execute query: %v", err)
		}

		if len(response.Items) == 0 {
			return ErrNotFound
		}

		// Unmarshal first result
		err = json.Unmarshal(response.Items[0], dest)
		if err != nil {
			return fmt.Errorf("failed to unmarshal result: %v", err)
		}

		return nil
	}

	return ErrNotFound
}

func (s *CosmosDBAdapter) Update(item any, filter map[string]any) error {
	if len(filter) == 0 {
		return fmt.Errorf("filtering is required when updating a resource")
	}

	containerName := s.getContainerName(item)
	container, err := s.client.NewContainer(s.config["database"], containerName)
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}

	// First get the item to update
	var existingItem map[string]interface{}
	err = s.Get(&existingItem, filter)
	if err != nil {
		return err
	}

	// Merge with new item
	itemMap := s.itemToMap(item)
	for key, value := range itemMap {
		existingItem[key] = value
	}

	// Update timestamp
	existingItem["_ts"] = time.Now().Unix()

	// Marshal item to JSON
	itemBytes, err := json.Marshal(existingItem)
	if err != nil {
		return fmt.Errorf("failed to marshal item: %v", err)
	}

	id, exists := existingItem["id"]
	if !exists {
		return fmt.Errorf("item does not have an id field")
	}

	_, err = container.ReplaceItem(context.TODO(), azcosmos.NewPartitionKeyString(""), id.(string), itemBytes, nil)
	if err != nil {
		return fmt.Errorf("failed to update item: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) Delete(item any, filter map[string]any) error {
	if len(filter) == 0 {
		return fmt.Errorf("filtering is required when deleting a resource")
	}

	containerName := s.getContainerName(item)
	container, err := s.client.NewContainer(s.config["database"], containerName)
	if err != nil {
		return fmt.Errorf("failed to get container: %v", err)
	}

	// First get the item to get its ID
	var existingItem map[string]interface{}
	err = s.Get(&existingItem, filter)
	if err != nil {
		return err
	}

	id, exists := existingItem["id"]
	if !exists {
		return fmt.Errorf("item does not have an id field")
	}

	_, err = container.DeleteItem(context.TODO(), azcosmos.NewPartitionKeyString(""), id.(string), nil)
	if err != nil {
		return fmt.Errorf("failed to delete item: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) executePaginatedQuery(
	dest any,
	limit int,
	cursor string,
	builder func(*azcosmos.QueryOptions) *azcosmos.QueryOptions,
) (string, error) {
	containerName := s.getContainerName(dest)
	container, err := s.client.NewContainer(s.config["database"], containerName)
	if err != nil {
		return "", fmt.Errorf("failed to get container: %v", err)
	}

	queryOptions := &azcosmos.QueryOptions{}

	if cursor != "" {
		queryOptions.ContinuationToken = &cursor
	}

	queryOptions = builder(queryOptions)

	pager := container.NewQueryItemsPager("SELECT * FROM c", azcosmos.NewPartitionKeyString(""), queryOptions)

	if pager.More() {
		response, err := pager.NextPage(context.TODO())
		if err != nil {
			return "", fmt.Errorf("failed to execute query: %v", err)
		}

		// Unmarshal results - response.Items is [][]byte, we need to convert to []json.RawMessage
		var items []json.RawMessage
		for _, itemBytes := range response.Items {
			items = append(items, itemBytes)
		}
		itemsJSON, err := json.Marshal(items)
		if err != nil {
			return "", fmt.Errorf("failed to marshal items: %v", err)
		}
		err = json.Unmarshal(itemsJSON, dest)
		if err != nil {
			return "", fmt.Errorf("failed to unmarshal results: %v", err)
		}

		nextCursor := ""
		if response.ContinuationToken != nil {
			nextCursor = *response.ContinuationToken
		}

		return nextCursor, nil
	}

	return "", nil
}

func (s *CosmosDBAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string) (string, error) {
	return s.executePaginatedQuery(dest, limit, cursor, func(queryOptions *azcosmos.QueryOptions) *azcosmos.QueryOptions {
		// For now, implement basic listing without filtering
		// TODO: Implement proper filtering and sorting
		return queryOptions
	})
}

func (s *CosmosDBAdapter) Search(dest any, sortKey string, query string, limit int, cursor string) (string, error) {
	return s.executePaginatedQuery(dest, limit, cursor, func(queryOptions *azcosmos.QueryOptions) *azcosmos.QueryOptions {
		// For now, implement basic text search
		// TODO: Implement proper Lucene query parsing for CosmosDB SQL
		if query != "" {
			// Simple contains search
			queryOptions.QueryParameters = append(queryOptions.QueryParameters, azcosmos.QueryParameter{
				Name:  "@searchTerm",
				Value: query,
			})
		}

		return queryOptions
	})
}

func (s *CosmosDBAdapter) Count(dest any) (int64, error) {
	containerName := s.getContainerName(dest)
	container, err := s.client.NewContainer(s.config["database"], containerName)
	if err != nil {
		return 0, fmt.Errorf("failed to get container: %v", err)
	}

	queryOptions := &azcosmos.QueryOptions{}
	pager := container.NewQueryItemsPager("SELECT VALUE COUNT(1) FROM c", azcosmos.NewPartitionKeyString(""), queryOptions)

	if pager.More() {
		response, err := pager.NextPage(context.TODO())
		if err != nil {
			return 0, fmt.Errorf("failed to execute count query: %v", err)
		}

		if len(response.Items) > 0 {
			var count int64
			err = json.Unmarshal(response.Items[0], &count)
			if err != nil {
				return 0, fmt.Errorf("failed to unmarshal count: %v", err)
			}
			return count, nil
		}
	}

	return 0, nil
}

func (s *CosmosDBAdapter) Query(dest any, statement string, limit int, cursor string) (string, error) {
	containerName := s.getContainerName(dest)
	container, err := s.client.NewContainer(s.config["database"], containerName)
	if err != nil {
		return "", fmt.Errorf("failed to get container: %v", err)
	}

	queryOptions := &azcosmos.QueryOptions{}

	if cursor != "" {
		queryOptions.ContinuationToken = &cursor
	}

	pager := container.NewQueryItemsPager(statement, azcosmos.NewPartitionKeyString(""), queryOptions)

	if pager.More() {
		response, err := pager.NextPage(context.TODO())
		if err != nil {
			return "", fmt.Errorf("failed to execute query: %v", err)
		}

		// Unmarshal results - response.Items is [][]byte, we need to convert to []json.RawMessage
		var items []json.RawMessage
		for _, itemBytes := range response.Items {
			items = append(items, itemBytes)
		}
		itemsJSON, err := json.Marshal(items)
		if err != nil {
			return "", fmt.Errorf("failed to marshal items: %v", err)
		}
		err = json.Unmarshal(itemsJSON, dest)
		if err != nil {
			return "", fmt.Errorf("failed to unmarshal results: %v", err)
		}

		nextCursor := ""
		if response.ContinuationToken != nil {
			nextCursor = *response.ContinuationToken
		}

		return nextCursor, nil
	}

	return "", nil
}

func (s *CosmosDBAdapter) getContainerName(obj any) string {
	// Get the type of obj
	tableName := ""
	tableName = reflect.TypeOf(obj).String()
	tableName = tableName[strings.LastIndex(tableName, ".")+1:]

	// Convert the table name to snake case
	matchFirstCap := regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllCap := regexp.MustCompile("([a-z0-9])([A-Z])")
	tableName = matchFirstCap.ReplaceAllString(tableName, "${1}_${2}")
	tableName = matchAllCap.ReplaceAllString(tableName, "${1}_${2}")

	tableName = strings.ToLower(tableName)
	tableName += "s"
	return tableName
}

func (s *CosmosDBAdapter) itemToMap(item any) map[string]interface{} {
	itemBytes, _ := json.Marshal(item)
	var itemMap map[string]interface{}
	json.Unmarshal(itemBytes, &itemMap)
	return itemMap
}

func (s *CosmosDBAdapter) mapToItem(itemMap map[string]interface{}, itemType reflect.Type) interface{} {
	itemBytes, _ := json.Marshal(itemMap)
	item := reflect.New(itemType).Interface()
	json.Unmarshal(itemBytes, item)
	return item
}
