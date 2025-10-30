package storage

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/tink3rlabs/magic/logger"
)

type CosmosDBAdapter struct {
	client         *azcosmos.Client
	databaseClient *azcosmos.DatabaseClient
	config         map[string]string
	databaseName   string
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
	var endpoint string
	var key string
	var databaseName string

	if connStr, exists := s.config["connection_string"]; exists {
		// Use connection string directly
		endpoint = connStr
	} else {
		// Build from individual parameters
		endpoint = s.config["endpoint"]
		key = s.config["key"]
		databaseName = s.config["database"]
		if databaseName == "" {
			databaseName = "magic"
		}

		if endpoint == "" || key == "" {
			logger.Fatal("CosmosDB endpoint and key are required")
		}
	}

	s.databaseName = databaseName

	// Check if TLS verification should be skipped (for local testing)
	skipTLS := s.config["skip_tls_verify"] == "true" || s.config["skip_tls_verify"] == "1"

	// Create Azure Cosmos DB client
	var err error
	var clientOptions *azcosmos.ClientOptions

	if skipTLS {
		// Configure client to skip TLS verification
		clientOptions = &azcosmos.ClientOptions{
			ClientOptions: azcore.ClientOptions{
				Transport: &http.Client{
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							InsecureSkipVerify: true,
						},
					},
				},
			},
		}
		slog.Warn("TLS verification is disabled - only use this for local testing!")
	}

	if key != "" {
		// Use account key authentication
		keyCredential, keyErr := azcosmos.NewKeyCredential(key)
		if keyErr != nil {
			logger.Fatal("Failed to create key credential", slog.Any("error", keyErr.Error()))
		}
		s.client, err = azcosmos.NewClientWithKey(endpoint, keyCredential, clientOptions)
	} else {
		// Use Azure AD authentication
		credential, credErr := azidentity.NewDefaultAzureCredential(nil)
		if credErr != nil {
			logger.Fatal("Failed to obtain Azure credential", slog.Any("error", credErr.Error()))
		}
		s.client, err = azcosmos.NewClient(endpoint, credential, clientOptions)
	}

	if err != nil {
		logger.Fatal("Failed to create CosmosDB client", slog.Any("error", err.Error()))
	}

	// Get database client
	s.databaseClient, err = s.client.NewDatabase(databaseName)
	if err != nil {
		logger.Fatal("Failed to create database client", slog.Any("error", err.Error()))
	}

	slog.Debug("Connected to CosmosDB using Azure SDK")
}

func (s *CosmosDBAdapter) Execute(statement string) error {
	// Azure SDK doesn't support arbitrary SQL execution like gocosmos
	// This method is kept for compatibility but will return an error
	return fmt.Errorf("Execute method not supported with Azure SDK - use specific CRUD methods instead")
}

func (s *CosmosDBAdapter) Ping() error {
	// Test connection by trying to read database properties
	_, err := s.databaseClient.Read(context.Background(), nil)
	return err
}

func (s *CosmosDBAdapter) GetType() StorageAdapterType {
	return COSMOSDB
}

func (s *CosmosDBAdapter) GetProvider() StorageProviders {
	return COSMOSDB_PROVIDER
}

func (s *CosmosDBAdapter) GetSchemaName() string {
	return s.databaseName
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

func (s *CosmosDBAdapter) Create(item any, params ...map[string]any) error {
	// Extract provider-specific parameters
	paramMap := s.extractParams(params...)

	containerName := s.getContainerName(item)
	containerClient, err := s.databaseClient.NewContainer(containerName)
	if err != nil {
		return fmt.Errorf("failed to create container client: %v", err)
	}

	// Convert item to map to work with individual fields
	itemMap := s.itemToMap(item)

	// Ensure id field exists
	if _, exists := itemMap["id"]; !exists {
		return fmt.Errorf("item must have an id field")
	}

	// Build partition key from params if provided
	if pk, err := s.buildPartitionKey(paramMap); err != nil {
		return fmt.Errorf("failed to build partition key: %v", err)
	} else if pk != "" {
		// Set the partition key value in the item
		pkFieldName := s.getPartitionKeyFieldName(paramMap)
		itemMap[pkFieldName] = pk
	} else if _, exists := itemMap["pk"]; !exists {
		// If no partition key is provided and item doesn't have pk, use id as partition key
		if id, exists := itemMap["id"]; exists {
			itemMap["pk"] = id
		}
	}

	// Marshal item to JSON
	itemBytes, err := json.Marshal(itemMap)
	if err != nil {
		return fmt.Errorf("failed to marshal item: %v", err)
	}

	// Get the partition key value from the item
	pkFieldName := s.getPartitionKeyFieldName(paramMap)
	var pkValue string
	if pk, exists := itemMap[pkFieldName]; exists {
		pkValue = pk.(string)
	} else if pk, exists := itemMap["pk"]; exists {
		// Fallback to "pk" field if custom field doesn't exist
		pkValue = pk.(string)
	} else {
		return fmt.Errorf("partition key field '%s' not found in item", pkFieldName)
	}

	// Create partition key
	partitionKey := azcosmos.NewPartitionKeyString(pkValue)

	// Create item
	_, err = containerClient.CreateItem(context.Background(), partitionKey, itemBytes, nil)
	if err != nil {
		return fmt.Errorf("failed to create item: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) Get(dest any, filter map[string]any, params ...map[string]any) error {
	if len(filter) == 0 {
		return fmt.Errorf("filtering is required when getting a resource")
	}

	// Extract provider-specific parameters
	paramMap := s.extractParams(params...)

	containerName := s.getContainerName(dest)
	containerClient, err := s.databaseClient.NewContainer(containerName)
	if err != nil {
		return fmt.Errorf("failed to create container client: %v", err)
	}

	// Build query
	query := "SELECT * FROM c"
	conditions := []string{}
	queryParams := []azcosmos.QueryParameter{}

	paramIndex := 1
	for key, value := range filter {
		paramName := fmt.Sprintf("@param%d", paramIndex)
		conditions = append(conditions, fmt.Sprintf("c.%s = %s", key, paramName))
		queryParams = append(queryParams, azcosmos.QueryParameter{
			Name:  paramName,
			Value: value,
		})
		paramIndex++
	}

	// Add partition key condition if provided in params
	if pk, err := s.buildPartitionKey(paramMap); err != nil {
		return fmt.Errorf("failed to build partition key: %v", err)
	} else if pk != "" {
		pkFieldName := s.getPartitionKeyFieldName(paramMap)
		paramName := fmt.Sprintf("@param%d", paramIndex)
		conditions = append(conditions, fmt.Sprintf("c.%s = %s", pkFieldName, paramName))
		queryParams = append(queryParams, azcosmos.QueryParameter{
			Name:  paramName,
			Value: pk,
		})
	}

	// Add WHERE clause if we have conditions
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Set up query options with parameters
	queryOptions := &azcosmos.QueryOptions{
		QueryParameters: queryParams,
	}

	// Execute query
	page, err := s.executeQuery(containerClient, query, paramMap, queryOptions)
	if err != nil {
		return fmt.Errorf("failed to execute query: %v", err)
	}

	if len(page.Items) == 0 {
		return ErrNotFound
	}

	// Unmarshal first result
	err = json.Unmarshal(page.Items[0], dest)
	if err != nil {
		return fmt.Errorf("failed to unmarshal result: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) Update(item any, filter map[string]any, params ...map[string]any) error {
	if len(filter) == 0 {
		return fmt.Errorf("filtering is required when updating a resource")
	}

	// Extract provider-specific parameters
	paramMap := s.extractParams(params...)

	containerName := s.getContainerName(item)
	containerClient, err := s.databaseClient.NewContainer(containerName)
	if err != nil {
		return fmt.Errorf("failed to create container client: %v", err)
	}

	// First get the item to update
	itemType := reflect.TypeOf(item)
	if itemType.Kind() == reflect.Ptr {
		itemType = itemType.Elem()
	}
	existingItem := reflect.New(itemType).Interface()

	err = s.Get(existingItem, filter, params...)
	if err != nil {
		return err
	}

	// Convert existing item to map for merging
	existingItemMap := s.itemToMap(existingItem)

	// Merge with new item
	itemMap := s.itemToMap(item)
	for key, value := range itemMap {
		existingItemMap[key] = value
	}

	// Update timestamp
	existingItemMap["_ts"] = time.Now().Unix()

	// Ensure id and pk fields exist
	id, exists := existingItemMap["id"]
	if !exists {
		return fmt.Errorf("item does not have an id field")
	}

	// Get the partition key field name
	pkFieldName := s.getPartitionKeyFieldName(paramMap)

	// Get or set the partition key value
	pk, exists := existingItemMap[pkFieldName]
	if !exists {
		// Check if partition key is provided in params
		if paramPk, err := s.buildPartitionKey(paramMap); err != nil {
			return fmt.Errorf("failed to build partition key: %v", err)
		} else if paramPk != "" {
			pk = paramPk
			existingItemMap[pkFieldName] = pk
		} else {
			// Fallback to "pk" field or id
			if fallbackPk, exists := existingItemMap["pk"]; exists {
				pk = fallbackPk
				existingItemMap[pkFieldName] = pk
			} else {
				pk = id
				existingItemMap[pkFieldName] = pk
			}
		}
	}

	// Marshal updated item
	itemBytes, err := json.Marshal(existingItemMap)
	if err != nil {
		return fmt.Errorf("failed to marshal item: %v", err)
	}

	// Create partition key
	partitionKey := azcosmos.NewPartitionKeyString(pk.(string))

	// Update item
	_, err = containerClient.ReplaceItem(context.Background(), partitionKey, id.(string), itemBytes, nil)
	if err != nil {
		return fmt.Errorf("failed to update item: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) Delete(item any, filter map[string]any, params ...map[string]any) error {
	if len(filter) == 0 {
		return fmt.Errorf("an id filter is required when deleting a resource")
	}

	// Extract provider-specific parameters
	paramMap := s.extractParams(params...)

	containerName := s.getContainerName(item)
	containerClient, err := s.databaseClient.NewContainer(containerName)
	if err != nil {
		return fmt.Errorf("failed to create container client: %v", err)
	}

	id := filter["id"]

	// Try to get partition key from params first
	pk, err := s.buildPartitionKey(paramMap)
	if err != nil {
		return fmt.Errorf("failed to build partition key: %v", err)
	}

	// If no partition key from params, try to get from filter
	if pk == "" {
		if filterPk, exists := filter["pk"]; exists {
			pk = filterPk.(string)
		} else {
			// Fallback to id
			pk = id.(string)
		}
	}

	// Create partition key
	partitionKey := azcosmos.NewPartitionKeyString(pk)

	// Delete item
	_, err = containerClient.DeleteItem(context.Background(), partitionKey, id.(string), nil)
	if err != nil {
		return fmt.Errorf("failed to delete item: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	// Extract sort direction from params
	paramMap := s.extractParams(params...)
	sortDirection := s.extractSortDirection(paramMap)

	return s.executePaginatedQuery(dest, sortKey, sortDirection, limit, cursor, filter, params...)
}

func (s *CosmosDBAdapter) Search(dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
	// Note: The Search method in CosmosDB is designed for full-text search scenarios
	// For CosmosDB, full-text search requires Azure Cognitive Search integration
	// This implementation treats Search as List with no filter
	// For custom queries, use the Query method instead

	// Extract sort direction from params
	paramMap := s.extractParams(params...)
	sortDirection := s.extractSortDirection(paramMap)

	// Use executePaginatedQuery with empty filter (the query parameter is ignored for CosmosDB)
	return s.executePaginatedQuery(dest, sortKey, sortDirection, limit, cursor, map[string]any{}, params...)
}

func (s *CosmosDBAdapter) Count(dest any, filter map[string]any, params ...map[string]any) (int64, error) {
	// TODO Implement
	var total int64
	return total, nil
}

func (s *CosmosDBAdapter) Query(dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
	// Note: For custom SQL queries, partition key parameters should be handled within the statement itself
	// The params are available but not automatically applied to the query
	// Users should include partition key conditions in their custom SQL statements when needed

	containerName := s.getContainerName(dest)
	containerClient, err := s.databaseClient.NewContainer(containerName)
	if err != nil {
		return "", fmt.Errorf("failed to create container client: %v", err)
	}

	// Set up query options
	enableCrossPartition := true
	queryOptions := &azcosmos.QueryOptions{
		EnableCrossPartitionQuery: &enableCrossPartition, // Enable cross-partition for custom queries
		PageSizeHint:              int32(limit),
	}

	// Handle cursor for pagination
	if cursor != "" {
		queryOptions.ContinuationToken = &cursor
	}

	// Execute the custom SQL statement
	// Cross-partition is enabled by default for custom queries
	pager := containerClient.NewQueryItemsPager(statement, azcosmos.NewPartitionKeyString(""), queryOptions)

	// Get first page
	page, err := pager.NextPage(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %v", err)
	}

	// Process results
	var results []json.RawMessage
	for _, item := range page.Items {
		results = append(results, json.RawMessage(item))
	}

	// Unmarshal results
	if len(results) > 0 {
		resultsJSON, err := json.Marshal(results)
		if err != nil {
			return "", fmt.Errorf("failed to marshal results: %v", err)
		}
		err = json.Unmarshal(resultsJSON, dest)
		if err != nil {
			return "", fmt.Errorf("failed to unmarshal results: %v", err)
		}
	}

	// Return continuation token for next page
	nextCursor := ""
	if page.ContinuationToken != nil {
		nextCursor = *page.ContinuationToken
	}

	return nextCursor, nil
}

func (s *CosmosDBAdapter) executePaginatedQuery(
	dest any,
	sortKey string,
	sortDirection string,
	limit int,
	cursor string,
	filter map[string]any,
	params ...map[string]any,
) (string, error) {
	// Extract provider-specific parameters
	paramMap := s.extractParams(params...)

	containerName := s.getContainerName(dest)
	containerClient, err := s.databaseClient.NewContainer(containerName)
	if err != nil {
		return "", fmt.Errorf("failed to create container client: %v", err)
	}

	// Build base query
	query := "SELECT * FROM c"
	queryParams := []azcosmos.QueryParameter{}
	paramIndex := 1

	// Build WHERE conditions
	conditions := []string{}

	// Add filter conditions if provided
	if len(filter) > 0 {
		filterClause, filterParams := s.buildFilter(filter, &paramIndex)
		if filterClause != "" {
			conditions = append(conditions, filterClause)
			queryParams = append(queryParams, filterParams...)
		}
	}

	// Add partition key condition if provided in params
	if pk, err := s.buildPartitionKey(paramMap); err != nil {
		return "", fmt.Errorf("failed to build partition key: %v", err)
	} else if pk != "" {
		pkFieldName := s.getPartitionKeyFieldName(paramMap)
		paramName := fmt.Sprintf("@param%d", paramIndex)
		conditions = append(conditions, fmt.Sprintf("c.%s = %s", pkFieldName, paramName))
		queryParams = append(queryParams, azcosmos.QueryParameter{
			Name:  paramName,
			Value: pk,
		})
		paramIndex++
	}

	// Add WHERE clause if we have conditions
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Add ordering - required for consistent pagination
	if sortKey != "" {
		query += fmt.Sprintf(" ORDER BY c.%s %s", sortKey, sortDirection)
	} else {
		// Use id as default sort key for consistent pagination
		query += fmt.Sprintf(" ORDER BY c.id %s", sortDirection)
	}

	// Set up query options
	queryOptions := &azcosmos.QueryOptions{
		QueryParameters: queryParams,
		PageSizeHint:    int32(limit),
	}

	// Handle cursor for pagination
	if cursor != "" {
		queryOptions.ContinuationToken = &cursor
	}

	// Execute query
	page, err := s.executeQuery(containerClient, query, paramMap, queryOptions)
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %v", err)
	}

	// Process results
	var results []json.RawMessage
	for _, item := range page.Items {
		results = append(results, json.RawMessage(item))
	}

	// Unmarshal results
	if len(results) > 0 {
		resultsJSON, err := json.Marshal(results)
		if err != nil {
			return "", fmt.Errorf("failed to marshal results: %v", err)
		}
		err = json.Unmarshal(resultsJSON, dest)
		if err != nil {
			return "", fmt.Errorf("failed to unmarshal results: %v", err)
		}
	}

	// Return continuation token for next page
	nextCursor := ""
	if page.ContinuationToken != nil {
		nextCursor = *page.ContinuationToken
	}

	return nextCursor, nil
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
	itemBytes, err := json.Marshal(item)
	if err != nil {
		return map[string]interface{}{}
	}
	var itemMap map[string]interface{}
	if err := json.Unmarshal(itemBytes, &itemMap); err != nil {
		return map[string]interface{}{}
	}
	return itemMap
}

// buildPartitionKey constructs a partition key from parameters
// Only supports explicit pk_field and pk_value parameters
func (s *CosmosDBAdapter) buildPartitionKey(paramMap map[string]any) (string, error) {
	// Check for pk_field and pk_value parameters
	if fieldName, exists := paramMap["pk_field"]; exists {
		if fieldStr, ok := fieldName.(string); ok && fieldStr != "" {
			if value, exists := paramMap["pk_value"]; exists {
				return fmt.Sprintf("%v", value), nil
			}
			return "", fmt.Errorf("pk_field specified but pk_value not found")
		}
		return "", fmt.Errorf("pk_field must be a non-empty string")
	}

	return "", nil // No partition key specified
}

// getPartitionKeyFieldName gets the partition key field name from params, defaulting to "pk"
func (s *CosmosDBAdapter) getPartitionKeyFieldName(paramMap map[string]any) string {
	if fieldName, exists := paramMap["pk_field"]; exists {
		if fieldStr, ok := fieldName.(string); ok && fieldStr != "" {
			return fieldStr
		}
	}
	return "pk" // Default
}

// executeQuery executes a query and handles single-partition vs cross-partition logic
func (s *CosmosDBAdapter) executeQuery(
	containerClient *azcosmos.ContainerClient,
	query string,
	paramMap map[string]any,
	queryOptions *azcosmos.QueryOptions,
) (azcosmos.QueryItemsResponse, error) {
	// Determine if we need cross-partition query
	pk, err := s.buildPartitionKey(paramMap)
	if err != nil {
		return azcosmos.QueryItemsResponse{}, fmt.Errorf("failed to build partition key: %v", err)
	}

	// Get first page
	var page azcosmos.QueryItemsResponse
	if pk != "" {
		// Single partition query - use the partition key
		pager := containerClient.NewQueryItemsPager(query, azcosmos.NewPartitionKeyString(pk), queryOptions)
		page, err = pager.NextPage(context.Background())
	} else {
		// Cross-partition query
		enableCrossPartition := true
		queryOptions.EnableCrossPartitionQuery = &enableCrossPartition
		pager := containerClient.NewQueryItemsPager(query, azcosmos.NewPartitionKeyString(""), queryOptions)
		page, err = pager.NextPage(context.Background())
	}

	return page, err
}

// extractParams merges all provided parameter maps into a single map
func (s *CosmosDBAdapter) extractParams(params ...map[string]any) map[string]any {
	paramMap := make(map[string]any)
	for _, param := range params {
		for k, v := range param {
			paramMap[k] = v
		}
	}
	return paramMap
}

// extractSortDirection extracts and validates sort direction from params
func (s *CosmosDBAdapter) extractSortDirection(paramMap map[string]any) string {
	sortDirection := "ASC" // Default to ASC
	if dir, exists := paramMap["sort_direction"]; exists {
		if dirStr, ok := dir.(string); ok {
			sortDirection = strings.ToUpper(dirStr)
			if sortDirection != "ASC" && sortDirection != "DESC" {
				sortDirection = "ASC" // Fallback to ASC for invalid values
			}
		}
	}
	return sortDirection
}

// buildFilter constructs WHERE clause conditions from filter map
func (s *CosmosDBAdapter) buildFilter(filter map[string]any, paramIndex *int) (string, []azcosmos.QueryParameter) {
	conditions := []string{}
	queryParams := []azcosmos.QueryParameter{}

	for key, value := range filter {
		if reflect.ValueOf(value).Kind() == reflect.Slice {
			// Handle IN clause for slices
			slice := reflect.ValueOf(value)
			placeholders := make([]string, slice.Len())
			for i := 0; i < slice.Len(); i++ {
				paramName := fmt.Sprintf("@param%d", *paramIndex)
				placeholders[i] = paramName
				queryParams = append(queryParams, azcosmos.QueryParameter{
					Name:  paramName,
					Value: slice.Index(i).Interface(),
				})
				*paramIndex++
			}
			conditions = append(conditions, fmt.Sprintf("c.%s IN (%s)", key, strings.Join(placeholders, ", ")))
		} else {
			// Handle single value equality
			paramName := fmt.Sprintf("@param%d", *paramIndex)
			conditions = append(conditions, fmt.Sprintf("c.%s = %s", key, paramName))
			queryParams = append(queryParams, azcosmos.QueryParameter{
				Name:  paramName,
				Value: value,
			})
			*paramIndex++
		}
	}

	return strings.Join(conditions, " AND "), queryParams
}
