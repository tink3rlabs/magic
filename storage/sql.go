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
)

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

func (s *SQLAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string) (string, error) {
	nextId := ""

	id, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("failed to decode next cursor: %v", err)
	}

	// Get one extra item to be able to set that item's Id as the cursor for the next request
	// result := s.DB.Limit(limit+1).Where(s.buildQuery(filter), filter).Where(sortKey+" >= ?", string(id)).Find(dest)
	q := s.DB.Limit(limit+1).Where(sortKey+" >= ?", string(id))
	if len(filter) > 0 {
		q.Where(s.buildQuery(filter), filter)
	}
	result := q.Find(dest)

	// If we have a full list, set the Id of the extra last item as the next cursor and remove it from the list of items to return
	v := reflect.ValueOf(dest)
	if (v.Elem().Len()) == limit+1 {
		lastItem := v.Elem().Index(v.Elem().Len() - 1)
		nextId = base64.StdEncoding.EncodeToString([]byte(lastItem.FieldByName(sortKey).String()))
		// Check if the value is a pointer and if it's settable
		if v.Kind() == reflect.Ptr && v.Elem().CanSet() {
			v.Elem().Set(v.Elem().Slice(0, v.Elem().Len()-1))
		}
	}

	return nextId, result.Error
}

func (s *SQLAdapter) buildQuery(filter map[string]any) string {
	clauses := []string{}
	for key := range filter {
		clauses = append(clauses, fmt.Sprintf("%s = @%s", key, key))
	}
	return strings.Join(clauses, " AND ")
}
