package storage

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
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
	_, err := s.DB.ExecuteStatement(context.TODO(), &dynamodb.ExecuteStatementInput{Statement: &statement})
	if err != nil {
		return fmt.Errorf("failed to execute statement %s: %v", statement, err)
	}
	return nil
}

func (s *DynamoDBAdapter) Ping() error {
	// dynamodb is a managed service so as long as it responds to api calls we can consider it up
	_, err := s.DB.ListTables(context.TODO(), &dynamodb.ListTablesInput{})
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

func (s *DynamoDBAdapter) Create(item any) error {
	i, err := attributevalue.MarshalMapWithOptions(item, func(eo *attributevalue.EncoderOptions) { eo.TagKey = "json" })
	if err != nil {
		return fmt.Errorf("failed to marshal input item into dynamodb item, %v", err)
	}

	_, err = s.DB.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: aws.String(s.getTableName(item)),
		Item:      i,
	})

	if err != nil {
		return fmt.Errorf("failed to create or update item: %v", err)
	}

	return nil
}

func (s *DynamoDBAdapter) Get(dest any, filter map[string]any) error {
	key, err := attributevalue.MarshalMapWithOptions(filter, func(eo *attributevalue.EncoderOptions) { eo.TagKey = "json" })
	if err != nil {
		return fmt.Errorf("failed to marshal item id into dynamodb attribute, %v", err)
	}

	response, err := s.DB.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: aws.String(s.getTableName(dest)),
		Key:       key,
	})

	if err != nil {
		return fmt.Errorf("failed to get item, %v", err)
	}

	if response.Item == nil {
		return ErrNotFound
	} else {
		err = attributevalue.UnmarshalMap(response.Item, &dest)
		if err != nil {
			return fmt.Errorf("failed to unmarshal dynamodb Get result into dest, %v", err)
		}

		return nil
	}
}

func (s *DynamoDBAdapter) Update(item any, filter map[string]any) error {
	return s.Create(item)
}

func (s *DynamoDBAdapter) Delete(item any, filter map[string]any) error {
	key, err := attributevalue.MarshalMapWithOptions(filter, func(eo *attributevalue.EncoderOptions) { eo.TagKey = "json" })
	if err != nil {
		return fmt.Errorf("failed to marshal item id into dynamodb attribute, %v", err)
	}

	_, err = s.DB.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
		TableName: aws.String(s.getTableName(item)),
		Key:       key,
	})

	if err != nil {
		return fmt.Errorf("failed to delete item, %v", err)
	}

	return nil
}

func (s *DynamoDBAdapter) executePaginatedQuery(
	dest any,
	limit int,
	cursor string,
	builder dynamoQueryBuilder,
) (string, error) {
	input := &dynamodb.ExecuteStatementInput{
		Limit: aws.Int32(int32(limit + 1)), // Get one extra for cursor
	}

	if cursor != "" {
		input.NextToken = aws.String(cursor)
	}

	input = builder(input)

	response, err := s.DB.ExecuteStatement(context.TODO(), input)
	if err != nil {
		slog.Error("Query execution failed", "error", err)
		return "", err
	}

	nextToken := ""
	if response.NextToken != nil {
		nextToken = *response.NextToken
	}

	if len(response.Items) > limit {
		response.Items = response.Items[:limit]
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

func (s *DynamoDBAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string) (string, error) {
	return s.executePaginatedQuery(dest, limit, cursor, func(input *dynamodb.ExecuteStatementInput) *dynamodb.ExecuteStatementInput {
		query := fmt.Sprintf(`SELECT * FROM "%s"`, s.getTableName(dest))

		if len(filter) > 0 {
			params, _ := s.buildParams(filter)
			input.Parameters = params
			query += fmt.Sprintf(` WHERE %s`, s.buildFilter(filter))
		}

		if sortKey != "" {
			query += fmt.Sprintf(` ORDER BY %s`, sortKey)
		}

		input.Statement = aws.String(query)
		return input
	})
}

func (s *DynamoDBAdapter) Search(dest any, sortKey string, query string, limit int, cursor string) (string, error) {
	return s.executePaginatedQuery(dest, limit, cursor, func(input *dynamodb.ExecuteStatementInput) *dynamodb.ExecuteStatementInput {
		// Parse Lucene query
		destType := reflect.TypeOf(dest).Elem().Elem()
		model := reflect.New(destType).Elem().Interface()
		parser, _ := lucene.NewParserFromType(model)
		whereClause, params, _ := parser.ParseToDynamoDBPartiQL(query)

		// Build query
		query := fmt.Sprintf(`SELECT * FROM "%s"`, s.getTableName(dest))
		if whereClause != "" {
			query += fmt.Sprintf(` WHERE %s`, whereClause)
		}
		if sortKey != "" {
			query += fmt.Sprintf(` ORDER BY %s`, sortKey)
		}

		input.Statement = aws.String(query)
		input.Parameters = params
		return input
	})
}

func (s *DynamoDBAdapter) Count(dest any) (int64, error) {
	// TODO Implement
	var total int64
	return total, nil
}

func (s *DynamoDBAdapter) Query(dest any, statement string, limit int, cursor string) (string, error) {
	next := ""
	input := &dynamodb.ExecuteStatementInput{
		Statement: aws.String(statement),
		Limit:     aws.Int32(int32(limit)),
	}
	if cursor != "" {
		input.NextToken = aws.String(cursor)
	}

	resp, err := s.DB.ExecuteStatement(context.TODO(), input)
	if err != nil {
		return "", fmt.Errorf("failed to execute query %s: %v", statement, err)
	}

	err = attributevalue.UnmarshalListOfMapsWithOptions(resp.Items, dest, func(eo *attributevalue.DecoderOptions) { eo.TagKey = "json" })
	if err != nil {
		return next, fmt.Errorf("failed to marshal query response items into destination, %v", err)
	}

	if resp.NextToken != nil {
		next = *resp.NextToken
	}

	return next, nil
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
