package storage

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "github.com/microsoft/gocosmos"

	"github.com/tink3rlabs/magic/logger"
)

type CosmosDBAdapter struct {
	db     *sql.DB
	config map[string]string
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
		// Use connection string directly
		endpoint = connStr
	} else {
		// Build DSN from individual parameters
		endpoint = s.config["endpoint"]
		key = s.config["key"]
		databaseName = s.config["database"]
		if databaseName == "" {
			databaseName = "magic"
		}

		if endpoint == "" || key == "" {
			logger.Fatal("CosmosDB endpoint and key are required")
		}

		// Build DSN for gocosmos
		endpoint = fmt.Sprintf("AccountEndpoint=%s;AccountKey=%s;DefaultDb=%s;AutoId=true", endpoint, key, databaseName)
	}

	// Open database connection using gocosmos driver
	db, err := sql.Open("gocosmos", endpoint)
	if err != nil {
		logger.Fatal("failed to open CosmosDB connection", slog.Any("error", err.Error()))
	}

	// Test the connection
	err = db.Ping()
	if err != nil {
		logger.Fatal("failed to ping CosmosDB", slog.Any("error", err.Error()))
	}

	s.db = db
	slog.Debug("Connected to CosmosDB using gocosmos driver")
}

func (s *CosmosDBAdapter) Execute(statement string) error {
	_, err := s.db.Exec(statement)
	if err != nil {
		return fmt.Errorf("failed to execute statement %s: %v", statement, err)
	}
	return nil
}

func (s *CosmosDBAdapter) Ping() error {
	return s.db.Ping()
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

func (s *CosmosDBAdapter) Create(item any, params ...map[string]any) error {
	// Extract provider-specific parameters
	paramMap := s.extractParams(params...)

	containerName := s.getContainerName(item)

	// Convert item to map to work with individual fields
	itemMap := s.itemToMap(item)

	// Build partition key from params if provided
	if pk, err := s.buildPartitionKey(paramMap); err != nil {
		return fmt.Errorf("failed to build partition key: %v", err)
	} else if pk != "" {
		itemMap["pk"] = pk
	} else if _, exists := itemMap["pk"]; !exists {
		// If no partition key is provided and item doesn't have pk, use id as partition key
		if id, exists := itemMap["id"]; exists {
			itemMap["pk"] = id
		}
	}

	// Build INSERT query with individual columns
	columns := make([]string, 0, len(itemMap))
	placeholders := make([]string, 0, len(itemMap))
	values := make([]interface{}, 0, len(itemMap))
	paramIndex := 1

	for key, value := range itemMap {
		columns = append(columns, key)
		placeholders = append(placeholders, fmt.Sprintf("@%d", paramIndex))
		values = append(values, value)
		paramIndex++
	}

	// Build the INSERT query
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		containerName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	// Execute the query
	_, err := s.db.Exec(query, values...)
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

	// Build query with gocosmos parameterized queries (@1, @2, etc.)
	query := "SELECT * FROM " + containerName + " c WHERE "
	conditions := []string{}
	args := []interface{}{}

	paramIndex := 1
	for key, value := range filter {
		conditions = append(conditions, fmt.Sprintf("c.%s = @%d", key, paramIndex))
		args = append(args, value)
		paramIndex++
	}

	// Add partition key condition if provided in params
	if pk, err := s.buildPartitionKey(paramMap); err != nil {
		return fmt.Errorf("failed to build partition key: %v", err)
	} else if pk != "" {
		conditions = append(conditions, fmt.Sprintf("c.pk = @%d", paramIndex))
		args = append(args, pk)
		paramIndex++
	}

	query += strings.Join(conditions, " AND ")

	// Execute query with parameters
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to execute query: %v", err)
	}
	defer rows.Close()

	// Check if rows has data
	if !rows.Next() {
		return ErrNotFound
	}

	// Get column information
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %v", err)
	}

	// gocosmos returns individual columns for each field in the document
	// We need to build a map from these columns and then marshal/unmarshal to handle custom types
	values := make([]interface{}, len(columns))
	scanArgs := make([]interface{}, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	// Scan all columns
	err = rows.Scan(scanArgs...)
	if err != nil {
		return fmt.Errorf("failed to scan result: %v", err)
	}

	// Build a map from columns to values
	resultMap := make(map[string]interface{})
	for i, col := range columns {
		resultMap[col] = values[i]
	}

	// Marshal the map to JSON and then unmarshal to dest
	// This allows datatypes.JSONMap and other custom types to handle the data correctly
	jsonBytes, err := json.Marshal(resultMap)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %v", err)
	}

	err = json.Unmarshal(jsonBytes, dest)
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

	// First get the item to update - use the same type as the input item
	itemType := reflect.TypeOf(item)
	if itemType.Kind() == reflect.Ptr {
		itemType = itemType.Elem()
	}
	existingItem := reflect.New(itemType).Interface()

	err := s.Get(existingItem, filter)
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

	// Build UPDATE query with individual fields like Create method
	// Exclude id field from updates since it should be immutable
	setClauses := make([]string, 0, len(existingItemMap))
	values := make([]interface{}, 0, len(existingItemMap))
	paramIndex := 1

	for key, value := range existingItemMap {
		// Skip id field - it should be immutable
		if key == "id" {
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = @%d", key, paramIndex))
		values = append(values, value)
		paramIndex++
	}

	// Add WHERE clause for id and partition key (required by CosmosDB)
	id, exists := existingItemMap["id"]
	if !exists {
		return fmt.Errorf("item does not have an id field")
	}

	// Use partition key from params if provided, otherwise use id as partition key
	pk, exists := existingItemMap["pk"]
	if !exists {
		// Check if partition key is provided in params
		if paramPk, err := s.buildPartitionKey(paramMap); err != nil {
			return fmt.Errorf("failed to build partition key: %v", err)
		} else if paramPk != "" {
			pk = paramPk
		} else {
			pk = id
		}
	}

	whereClause := fmt.Sprintf("id = @%d AND pk = @%d", paramIndex, paramIndex+1)
	values = append(values, id, pk)

	// Build the UPDATE query
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		containerName,
		strings.Join(setClauses, ", "),
		whereClause)

	// Execute the query
	_, err = s.db.Exec(query, values...)
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
	query := "DELETE FROM " + containerName + " WHERE id=@1 AND pk=@2"

	id := filter["id"]
	pk, exists := filter["pk"]
	if !exists {
		// Check if partition key is provided in params
		if paramPk, err := s.buildPartitionKey(paramMap); err != nil {
			return fmt.Errorf("failed to build partition key: %v", err)
		} else if paramPk != "" {
			pk = paramPk
		} else {
			pk = filter["id"]
		}
	}

	// Execute DELETE query with parameters
	_, err := s.db.Exec(query, id, pk)
	if err != nil {
		return fmt.Errorf("failed to delete item: %v", err)
	}

	return nil
}

func (s *CosmosDBAdapter) executePaginatedQuery(
	dest any,
	sortKey string,
	sortDirection string,
	limit int,
	cursor string,
	filter map[string]any,
	builder func(*sql.Rows) (*sql.Rows, error),
	params ...map[string]any,
) (string, error) {
	// Extract provider-specific parameters
	paramMap := s.extractParams(params...)

	containerName := s.getContainerName(dest)

	// Check if we need CROSS PARTITION based on partition key usage
	needsCrossPartition := true
	if pk, err := s.buildPartitionKey(paramMap); err == nil && pk != "" {
		// If partition key is specified, we can query within a single partition
		needsCrossPartition = false
	}

	// Build base query - use CROSS PARTITION only when needed
	var query string
	if needsCrossPartition {
		query = fmt.Sprintf("SELECT CROSS PARTITION * FROM %s c", containerName)
	} else {
		query = fmt.Sprintf("SELECT * FROM %s c", containerName)
	}

	// Build WHERE conditions
	conditions := []string{}
	args := []interface{}{}
	paramIndex := 1

	// Add filter conditions if provided
	if len(filter) > 0 {
		filterClause, filterArgs := s.buildFilter(filter, &paramIndex)
		if filterClause != "" {
			conditions = append(conditions, filterClause)
			args = append(args, filterArgs...)
		}
	}

	// Add partition key condition if provided in params
	if pk, err := s.buildPartitionKey(paramMap); err != nil {
		return "", fmt.Errorf("failed to build partition key: %v", err)
	} else if pk != "" {
		conditions = append(conditions, fmt.Sprintf("c.pk = @%d", paramIndex))
		args = append(args, pk)
		paramIndex++
	}

	// Handle pagination differently based on whether we need cross-partition queries
	if needsCrossPartition {
		// For cross-partition queries, we cannot use OFFSET/LIMIT
		// Instead, we use TOP with cursor-based pagination using WHERE clauses

		// Add cursor-based pagination using WHERE clause with sortKey comparison
		if cursor != "" {
			// Decode cursor to get the last value from previous page
			bytes, err := base64.StdEncoding.DecodeString(cursor)
			if err != nil {
				return "", fmt.Errorf("invalid cursor: %w", err)
			}
			cursorValue := string(bytes)

			// Add cursor condition - use appropriate operator based on sort direction
			operator := ">"
			if sortDirection == "DESC" {
				operator = "<"
			}

			if sortKey != "" {
				conditions = append(conditions, fmt.Sprintf("c.%s %s @%d", sortKey, operator, paramIndex))
			} else {
				conditions = append(conditions, fmt.Sprintf("c.id %s @%d", operator, paramIndex))
			}
			args = append(args, cursorValue)
			paramIndex++
		}

		// Add WHERE clause if we have conditions
		if len(conditions) > 0 {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}

		// Add ordering - required for cursor-based pagination
		if sortKey != "" {
			query += fmt.Sprintf(" ORDER BY c.%s %s", sortKey, sortDirection)
		} else {
			// Use id as default sort key for consistent pagination
			query += fmt.Sprintf(" ORDER BY c.id %s", sortDirection)
		}

		// Use TOP for cross-partition queries (get one extra to check for more results)
		query = strings.Replace(query, "SELECT CROSS PARTITION *", fmt.Sprintf("SELECT CROSS PARTITION TOP %d *", limit+1), 1)
	} else {
		// For single partition queries, we can use OFFSET and LIMIT (native pagination)

		// Add WHERE clause if we have conditions
		if len(conditions) > 0 {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}

		// Add ordering - CosmosDB requires ORDER BY for consistent pagination
		if sortKey != "" {
			query += fmt.Sprintf(" ORDER BY c.%s %s", sortKey, sortDirection)
		} else {
			// Use id as default sort key for consistent pagination
			query += fmt.Sprintf(" ORDER BY c.id %s", sortDirection)
		}

		// Use CosmosDB's native pagination with OFFSET and LIMIT
		// The cursor represents the offset for pagination
		offset := 0
		if cursor != "" {
			// Decode cursor to get offset
			bytes, err := base64.StdEncoding.DecodeString(cursor)
			if err != nil {
				return "", fmt.Errorf("invalid cursor: %w", err)
			}
			cursorValue := string(bytes)

			// Parse cursor as offset (simple approach)
			if parsedOffset, err := fmt.Sscanf(cursorValue, "%d", &offset); err != nil || parsedOffset != 1 {
				return "", fmt.Errorf("invalid cursor format: %w", err)
			}
		}

		// Add pagination clauses
		query += fmt.Sprintf(" OFFSET %d LIMIT %d", offset, limit)
	}

	// Execute query with parameters
	var rows *sql.Rows
	var err error
	if len(args) > 0 {
		rows, err = s.db.Query(query, args...)
	} else {
		rows, err = s.db.Query(query)
	}
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %v", err)
	}
	defer rows.Close()

	// Apply any additional filtering/sorting
	rows, err = builder(rows)
	if err != nil {
		return "", err
	}

	// Get column information
	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to get columns: %v", err)
	}

	// Process results
	var results []json.RawMessage
	var lastSortValue string
	resultCount := 0

	for rows.Next() {
		// Create a slice to hold all column values
		values := make([]interface{}, len(columns))
		scanArgs := make([]interface{}, len(columns))
		for i := range values {
			scanArgs[i] = &values[i]
		}

		// Scan all columns
		err = rows.Scan(scanArgs...)
		if err != nil {
			return "", fmt.Errorf("failed to scan result: %v", err)
		}

		// Build a map from columns to values
		resultMap := make(map[string]interface{})
		for i, col := range columns {
			resultMap[col] = values[i]
		}

		// Marshal the map to JSON
		jsonBytes, err := json.Marshal(resultMap)
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %v", err)
		}

		// Handle pagination differently based on query type
		if needsCrossPartition {
			// For cross-partition queries, only add up to the requested limit
			if len(results) < limit {
				results = append(results, json.RawMessage(jsonBytes))

				// Extract sortKey value from the result map for cursor generation
				if sortKey != "" {
					if sortVal, exists := resultMap[sortKey]; exists {
						if sortStr, ok := sortVal.(string); ok {
							lastSortValue = sortStr
						}
					}
				} else {
					if idVal, exists := resultMap["id"]; exists {
						if idStr, ok := idVal.(string); ok {
							lastSortValue = idStr
						}
					}
				}
			}
		} else {
			// For single partition queries, add all results (OFFSET/LIMIT handles pagination)
			results = append(results, json.RawMessage(jsonBytes))
		}
		resultCount++
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

	// Generate next cursor based on pagination type
	nextCursor := ""
	if needsCrossPartition {
		// For cross-partition queries, check if we got more than the limit (indicating more results)
		if resultCount > limit {
			// There are more results, encode the last sortKey value as cursor
			if lastSortValue != "" {
				nextCursor = base64.StdEncoding.EncodeToString([]byte(lastSortValue))
			}
		}
	} else {
		// For single partition queries, check if we got the full limit
		if resultCount == limit {
			// There might be more results, encode the next offset as cursor
			offset := 0
			if cursor != "" {
				bytes, err := base64.StdEncoding.DecodeString(cursor)
				if err == nil {
					cursorValue := string(bytes)
					fmt.Sscanf(cursorValue, "%d", &offset)
				}
			}
			nextOffset := offset + limit
			nextCursor = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", nextOffset)))
		}
	}

	return nextCursor, nil
}

func (s *CosmosDBAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	// Extract sort direction from params
	paramMap := s.extractParams(params...)
	sortDirection := s.extractSortDirection(paramMap)

	return s.executePaginatedQuery(dest, sortKey, sortDirection, limit, cursor, filter, func(rows *sql.Rows) (*sql.Rows, error) {
		// For now, return rows as-is
		// In a real implementation, you might want to apply additional filtering
		return rows, nil
	}, params...)
}

func (s *CosmosDBAdapter) Search(dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
	// Extract sort direction from params
	paramMap := s.extractParams(params...)
	sortDirection := s.extractSortDirection(paramMap)

	return s.executePaginatedQuery(dest, sortKey, sortDirection, limit, cursor, map[string]any{}, func(rows *sql.Rows) (*sql.Rows, error) {
		// For now, return rows as-is
		// In a real implementation, you might want to apply search filtering
		return rows, nil
	}, params...)
}

func (s *CosmosDBAdapter) Count(dest any, params ...map[string]any) (int64, error) {
	// Extract provider-specific parameters
	paramMap := s.extractParams(params...)

	containerName := s.getContainerName(dest)

	query := fmt.Sprintf("SELECT CROSS PARTITION COUNT(1) FROM %s c", containerName)
	args := []interface{}{}

	// Add partition key condition if provided in params
	if pk, err := s.buildPartitionKey(paramMap); err != nil {
		return 0, fmt.Errorf("failed to build partition key: %v", err)
	} else if pk != "" {
		query += " WHERE c.pk = @1"
		args = append(args, pk)
	}

	var count int64
	var err error
	if len(args) > 0 {
		err = s.db.QueryRow(query, args...).Scan(&count)
	} else {
		err = s.db.QueryRow(query).Scan(&count)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to execute count query: %v", err)
	}

	return count, nil
}

func (s *CosmosDBAdapter) Query(dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
	// Note: For custom SQL queries, partition key parameters should be handled within the statement itself
	// The params are available but not automatically applied to the query
	// Users should include partition key conditions in their custom SQL statements when needed

	// Execute the custom SQL statement
	rows, err := s.db.Query(statement)
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %v", err)
	}
	defer rows.Close()

	// Get column information
	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("failed to get columns: %v", err)
	}

	// Process results
	var results []json.RawMessage

	for rows.Next() {
		// Create a slice to hold all column values
		values := make([]interface{}, len(columns))
		scanArgs := make([]interface{}, len(columns))
		for i := range values {
			scanArgs[i] = &values[i]
		}

		// Scan all columns
		err = rows.Scan(scanArgs...)
		if err != nil {
			return "", fmt.Errorf("failed to scan result: %v", err)
		}

		// Build a map from columns to values
		resultMap := make(map[string]interface{})
		for i, col := range columns {
			resultMap[col] = values[i]
		}

		// Marshal the map to JSON
		jsonBytes, err := json.Marshal(resultMap)
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %v", err)
		}

		results = append(results, json.RawMessage(jsonBytes))
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

	return "", nil
}

func (s *CosmosDBAdapter) ensureContainerExists(containerName string) error {
	// Check if container exists
	query := fmt.Sprintf("SELECT COUNT(1) FROM %s c LIMIT 1", containerName)
	_, err := s.db.Exec(query)
	if err != nil {
		// Container doesn't exist, create it
		createQuery := fmt.Sprintf("CREATE COLLECTION %s WITH PK=id", containerName)
		_, err = s.db.Exec(createQuery)
		if err != nil {
			return fmt.Errorf("failed to create container %s: %v", containerName, err)
		}
	}
	return nil
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

// buildPartitionKey constructs a partition key from parameters
// Supports both single values and compound keys
func (s *CosmosDBAdapter) buildPartitionKey(paramMap map[string]any) (string, error) {
	// Check for direct pk parameter first
	if pk, exists := paramMap["pk"]; exists {
		if pkStr, ok := pk.(string); ok {
			return pkStr, nil
		}
		return "", fmt.Errorf("pk parameter must be a string")
	}

	// Check for compound partition key fields
	compoundFields := []string{"pk_fields", "partition_key_fields", "compound_pk"}
	for _, field := range compoundFields {
		if fields, exists := paramMap[field]; exists {
			if fieldList, ok := fields.([]string); ok {
				// Build synthetic partition key from multiple fields
				values := make([]string, 0, len(fieldList))
				for _, fieldName := range fieldList {
					if value, exists := paramMap[fieldName]; exists {
						values = append(values, fmt.Sprintf("%v", value))
					} else {
						return "", fmt.Errorf("compound partition key field '%s' not found in parameters", fieldName)
					}
				}
				return strings.Join(values, "_"), nil
			}
		}
	}

	// Check for individual compound fields (pk_field1, pk_field2, etc.)
	compoundValues := make([]string, 0)
	for key, value := range paramMap {
		if strings.HasPrefix(key, "pk_") && key != "pk" {
			compoundValues = append(compoundValues, fmt.Sprintf("%v", value))
		}
	}
	if len(compoundValues) > 0 {
		return strings.Join(compoundValues, "_"), nil
	}

	return "", nil // No partition key specified
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
func (s *CosmosDBAdapter) buildFilter(filter map[string]any, paramIndex *int) (string, []interface{}) {
	conditions := []string{}
	args := []interface{}{}

	for key, value := range filter {
		if reflect.ValueOf(value).Kind() == reflect.Slice {
			// Handle IN clause for slices
			slice := reflect.ValueOf(value)
			placeholders := make([]string, slice.Len())
			for i := 0; i < slice.Len(); i++ {
				placeholders[i] = fmt.Sprintf("@%d", *paramIndex)
				args = append(args, slice.Index(i).Interface())
				*paramIndex++
			}
			conditions = append(conditions, fmt.Sprintf("c.%s IN (%s)", key, strings.Join(placeholders, ", ")))
		} else {
			// Handle single value equality
			conditions = append(conditions, fmt.Sprintf("c.%s = @%d", key, *paramIndex))
			args = append(args, value)
			*paramIndex++
		}
	}

	return strings.Join(conditions, " AND "), args
}
