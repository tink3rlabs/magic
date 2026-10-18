package storage

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"regexp"
	"slices"
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

func (s *DynamoDBAdapter) Create(item any, params ...map[string]any) error {
	return s.CreateContext(context.Background(), item, params...)
}

func (s *DynamoDBAdapter) CreateContext(ctx context.Context, item any, params ...map[string]any) error {
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

func (s *DynamoDBAdapter) Get(dest any, filter map[string]any, params ...map[string]any) error {
	return s.GetContext(context.Background(), dest, filter, params...)
}

func (s *DynamoDBAdapter) GetContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) error {
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

func (s *DynamoDBAdapter) Update(item any, filter map[string]any, params ...map[string]any) error {
	return s.UpdateContext(context.Background(), item, filter, params...)
}

func (s *DynamoDBAdapter) UpdateContext(ctx context.Context, item any, filter map[string]any, params ...map[string]any) error {
	return s.CreateContext(ctx, item)
}

func (s *DynamoDBAdapter) Delete(item any, filter map[string]any, params ...map[string]any) error {
	return s.DeleteContext(context.Background(), item, filter, params...)
}

func (s *DynamoDBAdapter) DeleteContext(ctx context.Context, item any, filter map[string]any, params ...map[string]any) error {
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

func (s *DynamoDBAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	return s.ListContext(context.Background(), dest, sortKey, filter, limit, cursor, params...)
}

func (s *DynamoDBAdapter) ListContext(ctx context.Context, dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	if err := validateSortKey(sortKey); err != nil {
		return "", err
	}
	where, values, err := s.buildFilter(filter)
	if err != nil {
		return "", err
	}
	return s.executePaginatedQuery(ctx, dest, limit, cursor, func(input *dynamodb.ExecuteStatementInput) *dynamodb.ExecuteStatementInput {
		query := fmt.Sprintf(`SELECT * FROM "%s"`, s.getTableName(dest))

		if where != "" {
			input.Parameters = values
			query += fmt.Sprintf(` WHERE %s`, where)
		}

		if sortKey != "" {
			query += fmt.Sprintf(` ORDER BY %s`, sortKey)
		}

		input.Statement = aws.String(query)
		return input
	})
}

func (s *DynamoDBAdapter) Search(dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
	return s.SearchContext(context.Background(), dest, sortKey, query, limit, cursor, params...)
}

func (s *DynamoDBAdapter) SearchContext(ctx context.Context, dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
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

	return s.executePaginatedQuery(ctx, dest, limit, cursor, func(input *dynamodb.ExecuteStatementInput) *dynamodb.ExecuteStatementInput {
		// Build query
		query := fmt.Sprintf(`SELECT * FROM "%s"`, s.getTableName(dest))
		if whereClause != "" {
			query += fmt.Sprintf(` WHERE %s`, whereClause)
		}
		if sortKey != "" {
			query += fmt.Sprintf(` ORDER BY %s`, sortKey)
		}

		input.Statement = aws.String(query)
		input.Parameters = dynamoParams
		return input
	})
}

func (s *DynamoDBAdapter) Count(dest any, filter map[string]any, params ...map[string]any) (int64, error) {
	return s.CountContext(context.Background(), dest, filter, params...)
}

func (s *DynamoDBAdapter) CountContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) (int64, error) {
	// TODO Implement
	var total int64
	return total, nil
}

func (s *DynamoDBAdapter) Query(dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
	return s.QueryContext(context.Background(), dest, statement, limit, cursor, params...)
}

func (s *DynamoDBAdapter) QueryContext(ctx context.Context, dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
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

// buildFilter renders filter as a WHERE clause and the values for its "?"
// placeholders. PartiQL binds placeholders by position, so the clause and the
// values are built in one pass over the keys in sorted order: two separate
// passes over the map can visit the keys in different orders.
func (s *DynamoDBAdapter) buildFilter(filter map[string]any) (string, []types.AttributeValue, error) {
	keys := slices.Sorted(maps.Keys(filter))
	clauses := make([]string, 0, len(keys))
	values := make([]types.AttributeValue, 0, len(keys))

	for _, key := range keys {
		value := filter[key]
		rv := reflect.ValueOf(value)
		if rv.Kind() == reflect.Slice {
			placeholders := make([]string, rv.Len())
			for i := range placeholders {
				v, err := attributevalue.Marshal(rv.Index(i).Interface())
				if err != nil {
					return "", nil, fmt.Errorf("failed to marshal filter value for %q: %w", key, err)
				}
				placeholders[i] = "?"
				values = append(values, v)
			}
			clauses = append(clauses, fmt.Sprintf("%s IN (%s)", key, strings.Join(placeholders, ",")))
		} else {
			v, err := attributevalue.Marshal(value)
			if err != nil {
				return "", nil, fmt.Errorf("failed to marshal filter value for %q: %w", key, err)
			}
			clauses = append(clauses, fmt.Sprintf("%s=?", key))
			values = append(values, v)
		}
	}

	return strings.Join(clauses, " AND "), values, nil
}
