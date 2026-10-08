package storage

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
	"github.com/tink3rlabs/magic/logger"
)

type CosmosDBAdapter struct {
	client         *azcosmos.Client
	databaseClient *azcosmos.DatabaseClient
	config         map[string]string
	databaseName   string
	sessionTokens  *sessionTokenStore
}

var cosmosDBAdapterLock = &sync.Mutex{}
var cosmosDBAdapterInstance *CosmosDBAdapter

func GetCosmosDBAdapterInstance(config map[string]string) *CosmosDBAdapter {
	if cosmosDBAdapterInstance == nil {
		cosmosDBAdapterLock.Lock()
		defer cosmosDBAdapterLock.Unlock()
		if cosmosDBAdapterInstance == nil {
			cosmosDBAdapterInstance = &CosmosDBAdapter{config: config, sessionTokens: newSessionTokenStore()}
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
	skipTLS, _ := strconv.ParseBool(s.config["skip_tls_verify"])

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
		slog.Warn("TLS verification is disabled - use this only for local testing!")
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
	return s.ExecuteContext(context.Background(), statement)
}

func (s *CosmosDBAdapter) ExecuteContext(ctx context.Context, statement string) error {
	// Azure SDK doesn't support arbitrary SQL execution like gocosmos
	// This method is kept for compatibility but will return an error
	return fmt.Errorf("Execute method not supported with Azure SDK - use specific CRUD methods instead")
}

func (s *CosmosDBAdapter) Ping() error {
	return s.PingContext(context.Background())
}

func (s *CosmosDBAdapter) PingContext(ctx context.Context) error {
	// Test connection by trying to read database properties
	_, err := s.databaseClient.Read(ctx, nil)
	return err
}

func (s *CosmosDBAdapter) GetType() StorageAdapterType {
	return COSMOSDB
}

func (s *CosmosDBAdapter) GetProvider() StorageProviders {
	return COSMOSDB_PROVIDER
}

const cosmosSessionTokenHeader = "x-ms-session-token"

// recordSessionToken stores the session token from a write. A failed write still
// carries one in its HTTP response (for example a 409 when an SDK retry collides
// with an attempt that already landed), and that token is what lets the next read
// see the existing item.
func (s *CosmosDBAdapter) recordSessionToken(containerName string, pk string, token *string, err error) {
	if token == nil && err != nil {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) && respErr.RawResponse != nil {
			if t := respErr.RawResponse.Header.Get(cosmosSessionTokenHeader); t != "" {
				token = &t
			}
		}
	}
	s.setSessionToken(containerName, pk, token)
}

// setSessionToken stores token for the partition. The store is created with the
// adapter; a nil store means tracking is off.
func (s *CosmosDBAdapter) setSessionToken(containerName string, pk string, token *string) {
	if s.sessionTokens == nil || token == nil || *token == "" {
		return
	}
	s.sessionTokens.set(containerName, pk, *token)
}

// applySessionToken sets the session token on a read. A read scoped to one
// partition (pk != "") gets that partition's token; a read across partitions
// gets the newest token for every partition range of the container.
func (s *CosmosDBAdapter) applySessionToken(containerName string, pk string, queryOptions *azcosmos.QueryOptions) {
	if s.sessionTokens == nil {
		return
	}
	var token string
	if pk != "" {
		token = s.sessionTokens.get(containerName, pk)
	} else {
		token = s.sessionTokens.getForContainer(containerName)
	}
	if token != "" {
		queryOptions.SessionToken = &token
	}
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
	return s.CreateContext(context.Background(), item, params...)
}

func (s *CosmosDBAdapter) CreateContext(ctx context.Context, item any, params ...map[string]any) error {
	// Extract provider-specific parameters
	paramMap := extractParams(params...)

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
	response, err := containerClient.CreateItem(ctx, partitionKey, itemBytes, nil)
	s.recordSessionToken(containerName, pkValue, response.SessionToken, err)
	if err != nil {
		return fmt.Errorf("failed to create item: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) Get(dest any, filter map[string]any, params ...map[string]any) error {
	return s.GetContext(context.Background(), dest, filter, params...)
}

func (s *CosmosDBAdapter) GetContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) error {
	if len(filter) == 0 {
		return fmt.Errorf("filtering is required when getting a resource")
	}

	// Extract provider-specific parameters
	paramMap := extractParams(params...)

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
		ref, err := cosmosFilterRef(key)
		if err != nil {
			return err
		}
		condition, params := cosmosEquals(ref, value, &paramIndex)
		conditions = append(conditions, condition)
		queryParams = append(queryParams, params...)
	}

	// Add partition key condition if provided in params
	pk, err := s.buildPartitionKey(paramMap)
	if err != nil {
		return fmt.Errorf("failed to build partition key: %v", err)
	}
	if pk != "" {
		pkRef, err := s.partitionKeyRef(paramMap)
		if err != nil {
			return err
		}
		paramName := fmt.Sprintf("@param%d", paramIndex)
		conditions = append(conditions, fmt.Sprintf("%s = %s", pkRef, paramName))
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
	s.applySessionToken(containerName, pk, queryOptions)

	// Execute query
	page, err := s.executeQuery(ctx, containerClient, query, paramMap, queryOptions)
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
	return s.UpdateContext(context.Background(), item, filter, params...)
}

func (s *CosmosDBAdapter) UpdateContext(ctx context.Context, item any, filter map[string]any, params ...map[string]any) error {
	if len(filter) == 0 {
		return fmt.Errorf("filtering is required when updating a resource")
	}

	// Extract provider-specific parameters
	paramMap := extractParams(params...)

	containerName := s.getContainerName(item)
	containerClient, err := s.databaseClient.NewContainer(containerName)
	if err != nil {
		return fmt.Errorf("failed to create container client: %v", err)
	}

	// First get the item to update
	itemType := reflect.TypeOf(item)
	if itemType.Kind() == reflect.Pointer {
		itemType = itemType.Elem()
	}
	existingItem := reflect.New(itemType).Interface()

	err = s.GetContext(ctx, existingItem, filter, params...)
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
	response, err := containerClient.ReplaceItem(ctx, partitionKey, id.(string), itemBytes, nil)
	s.recordSessionToken(containerName, pk.(string), response.SessionToken, err)
	if err != nil {
		return fmt.Errorf("failed to update item: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) Delete(item any, filter map[string]any, params ...map[string]any) error {
	return s.DeleteContext(context.Background(), item, filter, params...)
}

func (s *CosmosDBAdapter) DeleteContext(ctx context.Context, item any, filter map[string]any, params ...map[string]any) error {
	if len(filter) == 0 {
		return fmt.Errorf("an id filter is required when deleting a resource")
	}

	// Extract provider-specific parameters
	paramMap := extractParams(params...)

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
	response, err := containerClient.DeleteItem(ctx, partitionKey, id.(string), nil)
	s.recordSessionToken(containerName, pk, response.SessionToken, err)
	if err != nil {
		return fmt.Errorf("failed to delete item: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	return s.ListContext(context.Background(), dest, sortKey, filter, limit, cursor, params...)
}

func (s *CosmosDBAdapter) ListContext(ctx context.Context, dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	// Extract sort direction from params
	paramMap := extractParams(params...)
	sortDirection, err := extractSortDirection(paramMap)
	if err != nil {
		return "", fmt.Errorf("failed to list: %w", err)
	}

	return s.executePaginatedQuery(ctx, dest, sortKey, sortDirection, limit, cursor, filter, params...)
}

func (s *CosmosDBAdapter) Search(dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
	return s.SearchContext(context.Background(), dest, sortKey, query, limit, cursor, params...)
}

func (s *CosmosDBAdapter) SearchContext(ctx context.Context, dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
	// Note: The Search method in CosmosDB is designed for full-text search scenarios
	// For CosmosDB, full-text search requires Azure Cognitive Search integration
	// This implementation treats Search as List with no filter
	// For custom queries, use the Query method instead

	// Extract sort direction from params
	paramMap := extractParams(params...)
	sortDirection, err := extractSortDirection(paramMap)
	if err != nil {
		return "", fmt.Errorf("failed to search: %w", err)
	}

	// Use executePaginatedQuery with empty filter (the query parameter is ignored for CosmosDB)
	return s.executePaginatedQuery(ctx, dest, sortKey, sortDirection, limit, cursor, map[string]any{}, params...)
}

func (s *CosmosDBAdapter) Count(dest any, filter map[string]any, params ...map[string]any) (int64, error) {
	return s.CountContext(context.Background(), dest, filter, params...)
}

// CountContext returns the number of documents in dest's partition that match
// filter. It runs a server-side aggregate (SELECT VALUE COUNT(1)), so no
// documents are transferred. An empty filter counts every document in the
// partition.
//
// pk_field and pk_value must be passed through params. The azcosmos SDK does
// not support cross-partition aggregates (the gateway rejects them), so an
// unscoped count returns an error instead of a wrong or failing query.
func (s *CosmosDBAdapter) CountContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) (int64, error) {
	if dest == nil {
		return 0, fmt.Errorf("a destination is required to count resources")
	}

	// Build and validate the query before touching the client so that bad
	// input fails without any network I/O.
	query, queryParams, pk, err := s.buildCountQuery(filter, extractParams(params...))
	if err != nil {
		return 0, err
	}

	containerClient, err := s.databaseClient.NewContainer(s.getContainerName(dest))
	if err != nil {
		return 0, fmt.Errorf("failed to create container client: %w", err)
	}

	queryOptions := &azcosmos.QueryOptions{
		QueryParameters: queryParams,
	}

	pager := containerClient.NewQueryItemsPager(query, azcosmos.NewPartitionKeyString(pk), queryOptions)
	return sumCountPages(ctx, pager)
}

// sumCountPages adds up the COUNT values from every page rather than trusting
// the first one, so a count that is split across pages is never under-reported.
func sumCountPages(ctx context.Context, pager *runtime.Pager[azcosmos.QueryItemsResponse]) (int64, error) {
	var total int64
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to execute count query: %w", err)
		}
		for _, raw := range page.Items {
			var n int64
			if err := json.Unmarshal(raw, &n); err != nil {
				return 0, fmt.Errorf("failed to parse count result: %w", err)
			}
			total += n
		}
	}
	return total, nil
}

// buildCountQuery builds the COUNT statement and its parameters, always scoped
// to the partition given by pk_field / pk_value, and returns the partition key
// value to run it against.
func (s *CosmosDBAdapter) buildCountQuery(filter map[string]any, paramMap map[string]any) (string, []azcosmos.QueryParameter, string, error) {
	pk, err := s.buildPartitionKey(paramMap)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to build partition key: %w", err)
	}
	if pk == "" {
		return "", nil, "", fmt.Errorf("cosmosdb count requires a non-empty pk_field and pk_value")
	}
	pkRef, err := s.partitionKeyRef(paramMap)
	if err != nil {
		return "", nil, "", err
	}

	query := "SELECT VALUE COUNT(1) FROM c"
	queryParams := []azcosmos.QueryParameter{}
	conditions := []string{}
	paramIndex := 1

	if len(filter) > 0 {
		clause, filterParams, err := s.buildFilter(filter, &paramIndex)
		if err != nil {
			return "", nil, "", err
		}
		if clause != "" {
			conditions = append(conditions, clause)
			queryParams = append(queryParams, filterParams...)
		}
	}

	paramName := fmt.Sprintf("@param%d", paramIndex)
	conditions = append(conditions, fmt.Sprintf("%s = %s", pkRef, paramName))
	queryParams = append(queryParams, azcosmos.QueryParameter{
		Name:  paramName,
		Value: pk,
	})

	query += " WHERE " + strings.Join(conditions, " AND ")

	return query, queryParams, pk, nil
}

func (s *CosmosDBAdapter) Query(dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
	return s.QueryContext(context.Background(), dest, statement, limit, cursor, params...)
}

func (s *CosmosDBAdapter) QueryContext(ctx context.Context, dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
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

	// The statement always runs across partitions, so it gets the token for
	// every partition range rather than one partition's
	s.applySessionToken(containerName, "", queryOptions)

	// Execute the custom SQL statement
	// Cross-partition is enabled by default for custom queries
	pager := containerClient.NewQueryItemsPager(statement, azcosmos.NewPartitionKeyString(""), queryOptions)

	// Get first page
	page, err := pager.NextPage(ctx)
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

// executePaginatedQuery runs a cursor-paginated Cosmos DB query against the container for dest.
// The cursor is a Cosmos DB continuation token. ctx is propagated to the Cosmos SDK so callers
// can cancel or deadline the operation and so tracing instrumentation has a parent span to use.
func (s *CosmosDBAdapter) executePaginatedQuery(
	ctx context.Context,
	dest any,
	sortKey string,
	sortDirection SortingDirection,
	limit int,
	cursor string,
	filter map[string]any,
	params ...map[string]any,
) (string, error) {
	if err := validateSortKey(sortKey); err != nil {
		return "", err
	}

	// Extract provider-specific parameters
	paramMap := extractParams(params...)

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
		filterClause, filterParams, err := s.buildFilter(filter, &paramIndex)
		if err != nil {
			return "", err
		}
		if filterClause != "" {
			conditions = append(conditions, filterClause)
			queryParams = append(queryParams, filterParams...)
		}
	}

	// Add partition key condition if provided in params
	pk, err := s.buildPartitionKey(paramMap)
	if err != nil {
		return "", fmt.Errorf("failed to build partition key: %v", err)
	}
	if pk != "" {
		pkRef, err := s.partitionKeyRef(paramMap)
		if err != nil {
			return "", err
		}
		paramName := fmt.Sprintf("@param%d", paramIndex)
		conditions = append(conditions, fmt.Sprintf("%s = %s", pkRef, paramName))
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
		sortRef, _ := cosmosFieldRef(sortKey) // validated by validateSortKey above
		query += fmt.Sprintf(" ORDER BY %s %s", sortRef, sortDirection)
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
	s.applySessionToken(containerName, pk, queryOptions)

	// Execute query
	page, err := s.executeQuery(ctx, containerClient, query, paramMap, queryOptions)
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
	ctx context.Context,
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
		page, err = pager.NextPage(ctx)
	} else {
		// Cross-partition query
		enableCrossPartition := true
		queryOptions.EnableCrossPartitionQuery = &enableCrossPartition
		pager := containerClient.NewQueryItemsPager(query, azcosmos.NewPartitionKeyString(""), queryOptions)
		page, err = pager.NextPage(ctx)
	}

	return page, err
}

// validCosmosFieldPath matches a plain identifier or a dotted path of them
// (e.g. "address.city"), which Cosmos resolves as a nested property.
var validCosmosFieldPath = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)*$`)

// cosmosFieldRef renders a field path as a quoted property accessor
// (c["address"]["city"]). Quoting keeps names that are Cosmos keywords, such
// as value, order or top, valid; validating the path first keeps the quoting
// safe, since no part can contain a quote or bracket.
func cosmosFieldRef(path string) (string, bool) {
	if !validCosmosFieldPath.MatchString(path) {
		return "", false
	}
	return `c["` + strings.ReplaceAll(path, ".", `"]["`) + `"]`, true
}

// cosmosFilterRef is cosmosFieldRef for a filter key, with the error to return
// when the key is not a valid path.
func cosmosFilterRef(key string) (string, error) {
	ref, ok := cosmosFieldRef(key)
	if !ok {
		return "", fmt.Errorf("invalid filter key %q: must be an identifier or dotted path of identifiers", key)
	}
	return ref, nil
}

// partitionKeyRef returns the property accessor for pk_field. Unlike filter
// keys it must be a plain identifier: Create writes the partition value under
// that literal top-level key, so a dotted path would never match it.
func (s *CosmosDBAdapter) partitionKeyRef(paramMap map[string]any) (string, error) {
	name := s.getPartitionKeyFieldName(paramMap)
	if !validColumnName.MatchString(name) {
		return "", fmt.Errorf("invalid partition key field %q: must match [a-zA-Z_][a-zA-Z0-9_]*", name)
	}
	ref, _ := cosmosFieldRef(name)
	return ref, nil
}

// cosmosEquals builds an equality condition on ref. A nil value matches both
// an explicit null and a missing property, the same rows SQL's IS NULL finds;
// a plain "= null" would skip documents that never had the property.
func cosmosEquals(ref string, value any, paramIndex *int) (string, []azcosmos.QueryParameter) {
	if value == nil {
		return fmt.Sprintf("(NOT IS_DEFINED(%s) OR IS_NULL(%s))", ref, ref), nil
	}
	paramName := fmt.Sprintf("@param%d", *paramIndex)
	*paramIndex++
	return fmt.Sprintf("%s = %s", ref, paramName), []azcosmos.QueryParameter{{Name: paramName, Value: value}}
}

// buildFilter constructs WHERE clause conditions from filter map. Keys are
// written into the query text, so each must be a valid field path.
func (s *CosmosDBAdapter) buildFilter(filter map[string]any, paramIndex *int) (string, []azcosmos.QueryParameter, error) {
	conditions := []string{}
	queryParams := []azcosmos.QueryParameter{}

	for key, value := range filter {
		ref, err := cosmosFilterRef(key)
		if err != nil {
			return "", nil, err
		}
		if reflect.ValueOf(value).Kind() == reflect.Slice {
			// Handle IN clause for slices
			slice := reflect.ValueOf(value)
			if slice.Len() == 0 {
				// "IN ()" is a syntax error in Cosmos SQL; an empty set
				// matches nothing.
				conditions = append(conditions, "false")
				continue
			}
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
			conditions = append(conditions, fmt.Sprintf("%s IN (%s)", ref, strings.Join(placeholders, ", ")))
		} else {
			condition, params := cosmosEquals(ref, value, paramIndex)
			conditions = append(conditions, condition)
			queryParams = append(queryParams, params...)
		}
	}

	return strings.Join(conditions, " AND "), queryParams, nil
}
