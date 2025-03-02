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
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(s.config["region"]),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s.config["access_key"], s.config["secret_key"], "")),
	)

	if err != nil {
		logger.Fatal("failed to open a database connection", slog.Any("error", err.Error()))
	}

	s.DB = dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if s.config["endpoint"] != "" {
			o.BaseEndpoint = aws.String(s.config["endpoint"])
		}
	})
}

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
		return fmt.Errorf("failed to marshal inpu item into dynamodb item, %v", err)
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

func (s *DynamoDBAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string) (string, error) {
	nextToken := ""
	params, err := s.buildParams(filter)
	if err != nil {
		return "", err
	}

	query := fmt.Sprintf(`SELECT * FROM "%v"`, s.getTableName(dest))
	if len(filter) > 0 {
		query = query + fmt.Sprintf(` WHERE %s`, s.buildFilter(filter))
	}
	input := dynamodb.ExecuteStatementInput{
		Statement:  aws.String(query),
		Parameters: params,
		Limit:      aws.Int32(int32(limit)),
	}

	if cursor != "" {
		input.NextToken = &cursor
	}

	response, err := s.DB.ExecuteStatement(context.TODO(), &input)

	if err != nil {
		return nextToken, fmt.Errorf("failed to list items, %v", err)
	}

	err = attributevalue.UnmarshalListOfMapsWithOptions(response.Items, dest, func(eo *attributevalue.DecoderOptions) { eo.TagKey = "json" })
	if err != nil {
		return nextToken, fmt.Errorf("failed to marshal scan response into item list, %v", err)
	}

	if response.NextToken != nil {
		nextToken = *response.NextToken
	}

	return nextToken, nil
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
