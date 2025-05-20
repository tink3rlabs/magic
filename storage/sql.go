package storage

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	slogger "github.com/tink3rlabs/magic/logger"

	"github.com/tink3rlabs/magic/storage/search/lucene"
)

type queryBuilder func(*gorm.DB) *gorm.DB

type SQLAdapter struct {
	DB       *gorm.DB
	config   map[string]string
	provider StorageProviders
}

var sqlAdapterLock = &sync.Mutex{}
var sqlAdapterInstance *SQLAdapter

func GetSQLAdapterInstance(config map[string]string) *SQLAdapter {
	if sqlAdapterInstance == nil {
		sqlAdapterLock.Lock()
		defer sqlAdapterLock.Unlock()
		if sqlAdapterInstance == nil {
			sqlAdapterInstance = &SQLAdapter{config: config}
			sqlAdapterInstance.OpenConnection()
		}
	}
	return sqlAdapterInstance
}

func (s *SQLAdapter) OpenConnection() {
	var err error
	s.provider = StorageProviders(s.config["provider"])
	delete(s.config, "provider")

	gormConf := gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix:   fmt.Sprintf("%s.", s.config["schema"]),
			SingularTable: false,
		},
		Logger: logger.Default.LogMode(logger.Silent),
	}

	switch s.provider {
	case POSTGRESQL:
		dsn := new(bytes.Buffer)

		for key, value := range s.config {
			if key != "schema" {
				fmt.Fprintf(dsn, "%s=%s ", key, value)
			}
		}
		s.DB, err = gorm.Open(postgres.New(postgres.Config{DSN: dsn.String(), PreferSimpleProtocol: true}), &gormConf)
	case MYSQL:
		dsn := new(bytes.Buffer)
		fmt.Fprintf(dsn, "%s:%s@tcp(%s:%s)/%s", s.config["user"], s.config["password"], s.config["host"], s.config["port"], s.config["dbname"])
		s.DB, err = gorm.Open(mysql.New(mysql.Config{DSN: dsn.String()}), &gormConf)
	case SQLITE:
		path := "file::memory:?cache=shared"
		if s.config["path"] != "" {
			path = s.config["path"]
		}
		s.DB, err = gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	default:
		slogger.Fatal("this SQL provider is not supported, supported providers are: postgresql, mysql, and sqlite")

	}

	if err != nil {
		slogger.Fatal("failed to open a database connection", slog.Any("error", err.Error()))
	}
}

func (s *SQLAdapter) buildQuery(filter map[string]any) string {
	clauses := []string{}
	for key := range filter {
		clauses = append(clauses, fmt.Sprintf("%s = @%s", key, key))
	}
	return strings.Join(clauses, " AND ")
}

func (s *SQLAdapter) Execute(statement string) error {
	result := s.DB.Exec(statement)
	if result.Error != nil {
		return fmt.Errorf("failed to execute statement %s: %v", statement, result.Error)
	}
	return nil
}

func (s *SQLAdapter) Ping() error {
	db, err := s.DB.DB()
	if err != nil {
		return fmt.Errorf("failed to connect to database: %v", err)
	}
	return db.Ping()
}

func (s *SQLAdapter) GetType() StorageAdapterType {
	return SQL
}

func (s *SQLAdapter) GetProvider() StorageProviders {
	return s.provider
}

func (s *SQLAdapter) GetSchemaName() string {
	return s.config["schema"]
}

func (s *SQLAdapter) Create(item any) error {
	result := s.DB.Create(reflect.ValueOf(item).Interface())
	return result.Error
}

func (s *SQLAdapter) Get(dest any, filter map[string]any) error {
	if len(filter) == 0 {
		return errors.New("filtering is required when getting a resource")
	}
	result := s.DB.Where(s.buildQuery(filter), filter).Find(dest)
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return result.Error
}

func (s *SQLAdapter) Update(item any, filter map[string]any) error {
	if len(filter) == 0 {
		return errors.New("filtering is required when updating a resource")
	}
	result := s.DB.Where(s.buildQuery(filter), filter).Save(item)
	return result.Error
}

func (s *SQLAdapter) Delete(item any, filter map[string]any) error {
	if len(filter) == 0 {
		return errors.New("filtering is required when deleting a resource")
	}
	result := s.DB.Where(s.buildQuery(filter), filter).Delete(item)
	return result.Error
}

func (s *SQLAdapter) executePaginatedQuery(
	dest any,
	sortKey string,
	limit int,
	cursor string,
	builder queryBuilder,
) (string, error) {
	var cursorValue string
	if cursor != "" {
		bytes, err := base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			return "", fmt.Errorf("invalid cursor: %w", err)
		}
		cursorValue = string(bytes)
	}
	q := s.DB.Model(dest).Scopes(builder)

	q = q.Limit(limit + 1).Order(fmt.Sprintf("%s ASC", sortKey))

	if cursorValue != "" {
		q = q.Where(fmt.Sprintf("%s > ?", sortKey), cursorValue)
	}

	if result := q.Find(dest); result.Error != nil {
		slog.Error("Query execution failed", "error", result.Error)
		return "", result.Error
	}

	destSlice := reflect.ValueOf(dest).Elem()
	if destSlice.Len() == 0 {
		return "", nil
	}

	nextCursor := ""
	if destSlice.Len() > limit {
		lastItem := destSlice.Index(limit - 1)
		field := reflect.Indirect(lastItem).FieldByName(sortKey)
		if field.IsValid() && field.Kind() == reflect.String {
			nextCursor = base64.StdEncoding.EncodeToString([]byte(field.String()))
		}
		destSlice.Set(destSlice.Slice(0, limit))
	} else {
		nextCursor = ""
	}

	return nextCursor, nil
}

func (s *SQLAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string) (string, error) {
	return s.executePaginatedQuery(dest, sortKey, limit, cursor, func(q *gorm.DB) *gorm.DB {
		if len(filter) > 0 {
			return q.Where(s.buildQuery(filter), filter)
		}
		return q
	})
}

func (s *SQLAdapter) Search(dest any, sortKey string, query string, limit int, cursor string) (string, error) {
	return s.executePaginatedQuery(dest, sortKey, limit, cursor, func(q *gorm.DB) *gorm.DB {
		if query == "" {
			return q
		}

		destType := reflect.TypeOf(dest).Elem().Elem()
		model := reflect.New(destType).Elem().Interface()

		parser, err := lucene.NewParserFromType(model)
		if err != nil {
			slog.Error("Parser creation failed", "error", err)
			return q
		}

		whereClause, params, err := parser.ParseToSQL(query)
		if err != nil {
			slog.Error("Filter parsing failed", "error", err)
			return q
		}

		if whereClause != "" {
			return q.Where(whereClause, params...)
		}

		return q
	})
}

func (s *SQLAdapter) Count(dest any) (int64, error) {
	q := s.DB.Model(dest)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		slog.Error("Error finding count")
		return 0, err
	}
	return total, nil
}
