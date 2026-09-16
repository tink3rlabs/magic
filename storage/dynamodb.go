package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/tink3rlabs/magic/logger"
	"github.com/tink3rlabs/magic/storage/search/lucene"
)

type DynamoDBAdapter struct {
	DB     *dynamodb.Client
	config map[string]string

	// keySchema caches each table's key attribute names, hash first. Update
	// needs them to address the item and to keep key attributes out of the SET
	// clause, and a table's key schema cannot change once it exists.
	keySchemaMu sync.RWMutex
	keySchema   map[string][]string
}

var dynamoDBAdapterLock = &sync.Mutex{}
var dynamoDBAdapterInstance *DynamoDBAdapter

func GetDynamoDBAdapterInstance(config map[string]string) *DynamoDBAdapter {
	if dynamoDBAdapterInstance == nil {
		dynamoDBAdapterLock.Lock()
		defer dynamoDBAdapterLock.Unlock()
		if dynamoDBAdapterInstance == nil {
			dynamoDBAdapterInstance = &DynamoDBAdapter{config: config}
			dynamoDBAdapterInstance.OpenConnection()
		}
	}
	return dynamoDBAdapterInstance
}

func (s *DynamoDBAdapter) OpenConnection() {
	cfg, err := config.LoadDefaultConfig(context.TODO())

	if s.config["region"] != "" {
		slog.Debug(fmt.Sprintf("using region override: %s", s.config["region"]))
		cfg.Region = s.config["region"]
	}
	if (s.config["access_key"] != "") && s.config["secret_key"] != "" {
		slog.Debug("using credentials from config file")
		cfg.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			s.config["access_key"],
			s.config["secret_key"],
			"",
		))
	}

	if err != nil {
		logger.Fatal("failed to open a database connection", slog.Any("error", err.Error()))
	}

	s.DB = dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if s.config["endpoint"] != "" {
			slog.Debug(fmt.Sprintf("using endpoint override: %s", s.config["endpoint"]))
			o.BaseEndpoint = aws.String(s.config["endpoint"])
		}
	})
}

type dynamoQueryBuilder func(*dynamodb.ExecuteStatementInput) *dynamodb.ExecuteStatementInput

func (s *DynamoDBAdapter) Execute(statement string) error {
	return s.ExecuteContext(context.Background(), statement)
}

func (s *DynamoDBAdapter) ExecuteContext(ctx context.Context, statement string) error {
	_, err := s.DB.ExecuteStatement(ctx, &dynamodb.ExecuteStatementInput{Statement: &statement})
	if err != nil {
		return fmt.Errorf("failed to execute statement %s: %v", statement, err)
	}
	return nil
}

func (s *DynamoDBAdapter) Ping() error {
	return s.PingContext(context.Background())
}

func (s *DynamoDBAdapter) PingContext(ctx context.Context) error {
	// dynamodb is a managed service so as long as it responds to api calls we can consider it up
	_, err := s.DB.ListTables(ctx, &dynamodb.ListTablesInput{})
	return err
}

func (s *DynamoDBAdapter) GetType() StorageAdapterType {
	return DYNAMODB
}

func (s *DynamoDBAdapter) GetProvider() StorageProviders {
	return ""
}

func (s *DynamoDBAdapter) GetSchemaName() string {
	return ""
}

func (s *DynamoDBAdapter) CreateSchema() error {
	return fmt.Errorf("DynamoDB CreateSchema is not supported")
}

func (s *DynamoDBAdapter) CreateMigrationTable() error {
	return fmt.Errorf("DynamoDB CreateMigrationTable is not supported")
}

func (s *DynamoDBAdapter) UpdateMigrationTable(id int, name string, desc string) error {
	return fmt.Errorf("DynamoDB UpgradeMigrationTable is not supported")
}

func (s *DynamoDBAdapter) GetLatestMigration() (int, error) {
	return -1, fmt.Errorf("DynamoDB GetLatestMigration is not supported")
}

func (s *DynamoDBAdapter) Create(item any, opts ...Option) error {
	return s.CreateContext(context.Background(), item, opts...)
}

func (s *DynamoDBAdapter) CreateContext(ctx context.Context, item any, opts ...Option) error {
	i, err := attributevalue.MarshalMapWithOptions(item, func(eo *attributevalue.EncoderOptions) { eo.TagKey = "json" })
	if err != nil {
		return fmt.Errorf("failed to marshal input item into dynamodb item, %v", err)
	}

	_, err = s.DB.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.getTableName(item)),
		Item:      i,
	})

	if err != nil {
		return fmt.Errorf("failed to create or update item: %v", err)
	}

	return nil
}

func (s *DynamoDBAdapter) Get(dest any, filter map[string]any, opts ...Option) error {
	return s.GetContext(context.Background(), dest, filter, opts...)
}

func (s *DynamoDBAdapter) GetContext(ctx context.Context, dest any, filter map[string]any, opts ...Option) error {
	key, err := attributevalue.MarshalMapWithOptions(filter, func(eo *attributevalue.EncoderOptions) { eo.TagKey = "json" })
	if err != nil {
		return fmt.Errorf("failed to marshal item id into dynamodb attribute, %v", err)
	}

	response, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.getTableName(dest)),
		Key:       key,
	})

	if err != nil {
		return fmt.Errorf("failed to get item, %v", err)
	}

	if response.Item == nil {
		return ErrNotFound
	} else {
		err = attributevalue.UnmarshalMapWithOptions(response.Item, &dest, func(eo *attributevalue.DecoderOptions) { eo.TagKey = "json" })
		if err != nil {
			return fmt.Errorf("failed to unmarshal dynamodb Get result into dest, %v", err)
		}

		return nil
	}
}

func (s *DynamoDBAdapter) Update(item any, filter map[string]any, opts ...Option) error {
	return s.UpdateContext(context.Background(), item, filter, opts...)
}

// UpdateContext writes item to the entry its key identifies, and only if that
// entry also matches filter. It never creates one; use Create.
//
// This is an UpdateItem with an UpdateExpression rather than a PutItem, which
// is what lets it write some attributes and leave the rest of the stored entry
// alone. A PutItem cannot: it replaces the whole entry, so an attribute the
// caller never mentioned is deleted rather than preserved (#263).
//
// Without WithFields every attribute of the item is written, which matches the
// SQL adapter's default. With it, only the named ones are.
func (s *DynamoDBAdapter) UpdateContext(ctx context.Context, item any, filter map[string]any, opts ...Option) error {
	if len(filter) == 0 {
		return errors.New("filtering is required when updating a resource")
	}
	options, err := ResolveOptions(opts...)
	if err != nil {
		return fmt.Errorf("failed to update: %w", err)
	}

	table := s.getTableName(item)
	keyNames, err := s.keyAttributeNames(ctx, table)
	if err != nil {
		return err
	}
	attributes, err := attributevalue.MarshalMapWithOptions(item, useJSONTag)
	if err != nil {
		return fmt.Errorf("failed to marshal input item into dynamodb item, %v", err)
	}

	key := make(map[string]types.AttributeValue, len(keyNames))
	for _, name := range keyNames {
		value, present := attributes[name]
		if !present {
			return fmt.Errorf("a key attribute is required when updating a resource: %q is missing from the item", name)
		}
		key[name] = value
	}

	names := map[string]string{}
	values := map[string]types.AttributeValue{}

	// UpdateItem rejects a SET on a key attribute, so those are skipped rather
	// than rejected: with no WithFields the write set is every attribute of the
	// item, and the key is always among them.
	write := options.Fields
	if !options.FieldsSet {
		write = make([]string, 0, len(attributes))
		for name := range attributes {
			write = append(write, name)
		}
		sort.Strings(write) // a map range would vary the expression per call
	}
	assignments := []string{}
	for _, field := range write {
		if slices.Contains(keyNames, field) {
			continue
		}
		value, present := attributes[field]
		if !present {
			return fmt.Errorf("unknown field %q on %s", field, table)
		}
		name, placeholder := fmt.Sprintf("#s%d", len(assignments)), fmt.Sprintf(":s%d", len(assignments))
		names[name], values[placeholder] = field, value
		assignments = append(assignments, fmt.Sprintf("%s = %s", name, placeholder))
	}
	if len(assignments) == 0 {
		return errors.New("nothing to update: every field named is a key attribute")
	}

	// The entry must already exist -- UpdateItem would otherwise create it --
	// and must match the filter. Key attributes may appear in a condition even
	// though they may not be assigned, so the filter needs no special casing.
	names["#exists"] = keyNames[0]
	conditions := []string{"attribute_exists(#exists)"}
	for field, want := range filter {
		value, err := attributevalue.Marshal(want)
		if err != nil {
			return fmt.Errorf("failed to marshal filter value for %q, %v", field, err)
		}
		name, placeholder := fmt.Sprintf("#c%d", len(conditions)), fmt.Sprintf(":c%d", len(conditions))
		names[name], values[placeholder] = field, value
		conditions = append(conditions, fmt.Sprintf("%s = %s", name, placeholder))
	}

	_, err = s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(table),
		Key:                       key,
		UpdateExpression:          aws.String("SET " + strings.Join(assignments, ", ")),
		ConditionExpression:       aws.String(strings.Join(conditions, " AND ")),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
	})
	if err != nil {
		// The condition covers both "no such entry" and "the filter did not
		// match", and DynamoDB reports them identically. ErrNotFound is what
		// the SQL adapter returns for either.
		var unmet *types.ConditionalCheckFailedException
		if errors.As(err, &unmet) {
			return ErrNotFound
		}
		return fmt.Errorf("failed to update item, %v", err)
	}
	return nil
}

// keyAttributeNames returns the table's key attribute names, hash key first.
// The schema is fixed for the life of a table, so it is fetched once per table
// and cached.
func (s *DynamoDBAdapter) keyAttributeNames(ctx context.Context, table string) ([]string, error) {
	s.keySchemaMu.RLock()
	cached, ok := s.keySchema[table]
	s.keySchemaMu.RUnlock()
	if ok {
		return cached, nil
	}

	described, err := s.DB.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(table)})
	if err != nil {
		return nil, fmt.Errorf("failed to describe table %s: %v", table, err)
	}
	var hash, sortKey string
	for _, element := range described.Table.KeySchema {
		if element.AttributeName == nil {
			continue
		}
		if element.KeyType == types.KeyTypeHash {
			hash = *element.AttributeName
		} else {
			sortKey = *element.AttributeName
		}
	}
	if hash == "" {
		return nil, fmt.Errorf("table %s reports no hash key", table)
	}
	names := []string{hash}
	if sortKey != "" {
		names = append(names, sortKey)
	}

	s.keySchemaMu.Lock()
	if s.keySchema == nil {
		s.keySchema = map[string][]string{}
	}
	s.keySchema[table] = names
	s.keySchemaMu.Unlock()
	return names, nil
}

// useJSONTag makes the attributevalue codec read `json` tags, which is how
// every other method in this adapter marshals.
func useJSONTag(eo *attributevalue.EncoderOptions) { eo.TagKey = "json" }

func (s *DynamoDBAdapter) Delete(item any, filter map[string]any, opts ...Option) error {
	return s.DeleteContext(context.Background(), item, filter, opts...)
}

func (s *DynamoDBAdapter) DeleteContext(ctx context.Context, item any, filter map[string]any, opts ...Option) error {
	key, err := attributevalue.MarshalMapWithOptions(filter, func(eo *attributevalue.EncoderOptions) { eo.TagKey = "json" })
	if err != nil {
		return fmt.Errorf("failed to marshal item id into dynamodb attribute, %v", err)
	}

	_, err = s.DB.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.getTableName(item)),
		Key:       key,
	})

	if err != nil {
		return fmt.Errorf("failed to delete item, %v", err)
	}

	return nil
}

func (s *DynamoDBAdapter) executePaginatedQuery(
	ctx context.Context,
	dest any,
	limit int,
	cursor string,
	builder dynamoQueryBuilder,
) (string, error) {
	input := &dynamodb.ExecuteStatementInput{
		Limit: aws.Int32(int32(limit)),
	}

	if cursor != "" {
		input.NextToken = aws.String(cursor)
	}

	input = builder(input)

	response, err := s.DB.ExecuteStatement(ctx, input)
	if err != nil {
		slog.Error("Query execution failed", "error", err)
		return "", err
	}

	nextToken := ""
	if response.NextToken != nil {
		nextToken = *response.NextToken
	}

	err = attributevalue.UnmarshalListOfMapsWithOptions(
		response.Items,
		dest,
		func(eo *attributevalue.DecoderOptions) { eo.TagKey = "json" },
	)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return nextToken, nil
}

func (s *DynamoDBAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string, opts ...Option) (string, error) {
	return s.ListContext(context.Background(), dest, sortKey, filter, limit, cursor, opts...)
}

func (s *DynamoDBAdapter) ListContext(ctx context.Context, dest any, sortKey string, filter map[string]any, limit int, cursor string, opts ...Option) (string, error) {
	if err := validateSortKey(sortKey); err != nil {
		return "", err
	}
	options, err := ResolveOptions(opts...)
	if err != nil {
		return "", fmt.Errorf("failed to list: %w", err)
	}
	if err := requireFilterWhenOrdering(sortKey, len(filter) > 0); err != nil {
		return "", err
	}
	return s.executePaginatedQuery(ctx, dest, limit, cursor, func(input *dynamodb.ExecuteStatementInput) *dynamodb.ExecuteStatementInput {
		query := fmt.Sprintf(`SELECT * FROM "%s"`, s.getTableName(dest))

		if len(filter) > 0 {
			params, _ := s.buildParams(filter)
			input.Parameters = params
			query += fmt.Sprintf(` WHERE %s`, s.buildFilter(filter))
		}

		if sortKey != "" {
			query += fmt.Sprintf(` ORDER BY %s %s`, sortKey, options.SortDirection)
		}

		input.Statement = aws.String(query)
		return input
	})
}

// requireFilterWhenOrdering rejects an ordered scan with nothing to scope it.
// PartiQL on DynamoDB refuses ORDER BY unless the statement also carries a
// WHERE, and the service reports that as an opaque ValidationException from
// inside ExecuteStatement. Failing here names the caller's actual mistake.
func requireFilterWhenOrdering(sortKey string, hasWhere bool) error {
	if sortKey == "" || hasWhere {
		return nil
	}
	return fmt.Errorf("ordering by %q requires a filter: DynamoDB rejects ORDER BY without a WHERE clause", sortKey)
}

func (s *DynamoDBAdapter) Search(dest any, sortKey string, query string, limit int, cursor string, opts ...Option) (string, error) {
	return s.SearchContext(context.Background(), dest, sortKey, query, limit, cursor, opts...)
}

func (s *DynamoDBAdapter) SearchContext(ctx context.Context, dest any, sortKey string, query string, limit int, cursor string, opts ...Option) (string, error) {
	if err := validateSortKey(sortKey); err != nil {
		return "", err
	}

	destType := reflect.TypeOf(dest).Elem().Elem()
	model := reflect.New(destType).Elem().Interface()
	parser, err := lucene.NewParser(model)
	if err != nil {
		return "", err
	}
	whereClause, dynamoParams, err := parser.ParseToDynamoDBPartiQL(query)
	if err != nil {
		return "", err
	}
	options, err := ResolveOptions(opts...)
	if err != nil {
		return "", fmt.Errorf("failed to search: %w", err)
	}
	if err := requireFilterWhenOrdering(sortKey, whereClause != ""); err != nil {
		return "", err
	}

	return s.executePaginatedQuery(ctx, dest, limit, cursor, func(input *dynamodb.ExecuteStatementInput) *dynamodb.ExecuteStatementInput {
		// Build query
		query := fmt.Sprintf(`SELECT * FROM "%s"`, s.getTableName(dest))
		if whereClause != "" {
			query += fmt.Sprintf(` WHERE %s`, whereClause)
		}
		if sortKey != "" {
			query += fmt.Sprintf(` ORDER BY %s %s`, sortKey, options.SortDirection)
		}

		input.Statement = aws.String(query)
		input.Parameters = dynamoParams
		return input
	})
}

func (s *DynamoDBAdapter) Count(dest any, filter map[string]any, opts ...Option) (int64, error) {
	return s.CountContext(context.Background(), dest, filter, opts...)
}

func (s *DynamoDBAdapter) CountContext(ctx context.Context, dest any, filter map[string]any, opts ...Option) (int64, error) {
	// TODO Implement
	var total int64
	return total, nil
}

func (s *DynamoDBAdapter) Query(dest any, statement string, limit int, cursor string, opts ...Option) (string, error) {
	return s.QueryContext(context.Background(), dest, statement, limit, cursor, opts...)
}

func (s *DynamoDBAdapter) QueryContext(ctx context.Context, dest any, statement string, limit int, cursor string, opts ...Option) (string, error) {
	return s.executePaginatedQuery(ctx, dest, limit, cursor, func(input *dynamodb.ExecuteStatementInput) *dynamodb.ExecuteStatementInput {
		input.Statement = aws.String(statement)
		return input
	})
}

func (s *DynamoDBAdapter) getTableName(obj any) string {
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

func (s *DynamoDBAdapter) buildFilter(filter map[string]any) string {
	clauses := []string{}
	for key, value := range filter {
		if reflect.ValueOf(value).Kind() == reflect.Slice {
			c := "IN ("
			len := reflect.ValueOf(value).Len()
			for i := 0; i < len; i++ {
				if i < len-1 {
					c += "?,"
				} else {
					c += "?)"
				}
			}
			clauses = append(clauses, fmt.Sprintf("%s %s", key, c))
		} else {
			clauses = append(clauses, fmt.Sprintf("%s=?", key))
		}
	}
	return strings.Join(clauses, " AND ")
}

func (s *DynamoDBAdapter) buildParams(filter map[string]any) ([]types.AttributeValue, error) {
	values := make([]types.AttributeValue, 0, len(filter))

	for _, value := range filter {
		if reflect.ValueOf(value).Kind() == reflect.Slice {
			len := reflect.ValueOf(value).Len()
			for i := 0; i < len; i++ {
				t := reflect.ValueOf(value).Index(i).Interface()
				v, err := attributevalue.Marshal(t)
				if err != nil {
					return values, err
				}
				values = append(values, v)
			}
		} else {
			v, err := attributevalue.Marshal(value)
			if err != nil {
				return values, err
			}
			values = append(values, v)
		}
	}

	return values, nil
}
