package storage

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/gocql/gocql"
	"github.com/scylladb/go-reflectx"
	"github.com/scylladb/gocqlx/v3"
	"github.com/scylladb/gocqlx/v3/qb"
	"github.com/scylladb/gocqlx/v3/table"
)

var (
	cassandraAdapterLock     = &sync.Mutex{}
	cassandraAdapterInstance *CassandraAdapter
)

const (
	hosts               = "hosts"
	keyspace            = "keyspace"
	tablesAutoDiscovery = "tables_auto_discovery"
	tables              = "tables"
	provider            = "provider"
)

type CassandraAdapter struct {
	StorageAdapter
	config        map[string]string
	clusterConfig *gocql.ClusterConfig
	provider      StorageProviders
	tables        map[string]*table.Table
}

func GetCassandraAdapter(config map[string]string) *CassandraAdapter {
	if cassandraAdapterInstance == nil {
		cassandraAdapterLock.Lock()
		defer cassandraAdapterLock.Unlock()
		if cassandraAdapterInstance == nil {
			cassandraAdapterInstance = &CassandraAdapter{config: config}
			cassandraAdapterInstance.initializeCluster()
			cassandraAdapterInstance.initializeTableMappers()
		}
	}
	return cassandraAdapterInstance

}

func (c *CassandraAdapter) GetSchemaName() string {
	return c.config[keyspace]
}

func (c *CassandraAdapter) GetProvider() StorageProviders {
	return c.provider
}

func (c *CassandraAdapter) GetType() StorageAdapterType {
	return NOSQL
}

func (c *CassandraAdapter) initializeCluster() {
	c.clusterConfig = &gocql.ClusterConfig{}
	c.clusterConfig.Hosts = strings.Split(c.config[hosts], ",")
	c.clusterConfig.Keyspace = c.config[keyspace]
}

func (c *CassandraAdapter) createSession() (gocqlx.Session, error) {
	return gocqlx.WrapSession(c.clusterConfig.CreateSession())
}

func (c *CassandraAdapter) initializeTableMappers() error {
	autoDiscoery, parseErr := strconv.ParseBool(c.config[tablesAutoDiscovery])
	if parseErr != nil {
		return parseErr
	}
	if !autoDiscoery {
		decodeErr := mapstructure.Decode(c.config[tables], c.tables)
		return decodeErr
	}
	s, e := c.createSession()
	if e != nil {
		return e
	}
	metadata, mErr := s.KeyspaceMetadata(c.config[keyspace])
	if mErr != nil {
		return mErr
	}

	for _, t := range metadata.Tables {
		tableMetadata := table.Metadata{
			Name:    t.Name,
			Columns: t.OrderedColumns,
		}
		if len(t.PartitionKey) > 0 {
			partitionKeys := []string{}
			for _, k := range t.PartitionKey {
				partitionKeys = append(partitionKeys, k.Name)
			}
			tableMetadata.PartKey = partitionKeys
		}
		if len(t.ClusteringColumns) > 0 {
			sortKeys := []string{}
			for _, k := range t.ClusteringColumns {
				sortKeys = append(sortKeys, k.Name)
			}
			tableMetadata.SortKey = sortKeys
		}
		c.tables[t.Name] = table.New(tableMetadata)
	}
	return nil
}

func (c *CassandraAdapter) getTableForItem(item any) (*table.Table, error) {
	typeOf := reflect.TypeOf(item)
	valueOf := reflect.ValueOf(item)
	if valueOf.Kind() == reflect.Pointer {
		slog.Debug("Dereferencing pointer", slog.String("pointer", valueOf.String()))
		elemVal := valueOf.Elem()
		if elemVal.Kind() == reflect.Slice {
			slog.Debug("Handling slice item", slog.String("slice", elemVal.String()))
			sliceType := elemVal.Type()
			elemTypeOfSlice := sliceType.Elem()
			if elemTypeOfSlice.Kind() == reflect.Pointer {
				slog.Debug("Dereferencing struct pointer",
					slog.String("sliceType", sliceType.String()),
					slog.String("elemeTypeOfSlice", elemTypeOfSlice.String()))
				typeOf = elemTypeOfSlice.Elem()
			}
		}
	}

	itemName := typeOf.Elem().Name()
	tableName := reflectx.CamelToSnakeASCII(itemName)
	if t, ok := c.tables[tableName]; !ok {
		return nil, fmt.Errorf("no table metadata for [%s]", tableName)
	} else {
		return t, nil
	}
}

func (c *CassandraAdapter) Execute(statement string) error {
	s, err := c.createSession()
	if err != nil {
		return err
	}
	defer s.Close()
	return s.ExecStmt(statement)
}

func (c *CassandraAdapter) Ping() error {
	s, err := c.createSession()
	if err != nil {
		return err
	}
	defer s.Close()
	return nil
}

func (c *CassandraAdapter) createSchema() error {
	replicationClass := "SimpleStrategy"
	replicationFactor := 1
	return c.Execute(
		fmt.Sprintf(
			"CREATE KEYSPACE IF NOT EXIST %s WITH REPLICATION = {'class':'%s', 'replication_factory': %d}",
			c.GetSchemaName(),
			replicationClass,
			replicationFactor,
		),
	)
}
func (c *CassandraAdapter) createMigrationTable() error {
	statement := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s.migrations (
			id DECIMAL PRIMARY KEY,
			name TEXT,
			description TEXT,
			timestamp DECIMAL)`,
		c.GetSchemaName())
	return c.Execute(statement)

}
func (c *CassandraAdapter) updateMigrationTable(id int, name string, desc string) error {
	t, e := c.getTableForItem("migrations")
	if e != nil {
		return e
	}
	s, sErr := c.createSession()
	if sErr != nil {
		return sErr
	}
	params := map[string]any{}
	params["id"] = id
	params["name"] = name
	params["description"] = desc
	params["timestamp"] = time.Now().UnixMilli()
	q := t.InsertQuery(s)
	q = q.BindMap(params)
	return q.ExecRelease()
}
func (c *CassandraAdapter) getLatestMigration() (int, error) {
	t, e := c.getTableForItem("migrations")
	if e != nil {
		return -1, e
	}
	s, sErr := c.createSession()
	if sErr != nil {
		return -1, sErr
	}
	var latestMigration int
	t.SelectBuilder("id").Max("id").Query(s).Bind(&latestMigration).ExecRelease()
	return latestMigration, nil
}

func (c *CassandraAdapter) Create(item any) error {
	s, err := c.createSession()
	if err != nil {
		return err
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(item)
	if tableErr != nil {
		return tableErr
	}
	return t.InsertQuery(s).BindStruct(item).ExecRelease()
}
func (c *CassandraAdapter) Get(dest any, filter map[string]any) error {
	s, err := c.createSession()
	if err != nil {
		return err
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(dest)
	if tableErr != nil {
		return tableErr
	}
	return t.GetQuery(s).BindStruct(dest).GetRelease(dest)
}
func (c *CassandraAdapter) Update(item any, filter map[string]any) error {
	s, err := c.createSession()
	if err != nil {
		return err
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(item)
	if tableErr != nil {
		return tableErr
	}

	marshalledItem, marshalErr := json.Marshal(item)
	if marshalErr != nil {
		return marshalErr
	}
	var jsonMap map[string]any
	json.Unmarshal(marshalledItem, &jsonMap)
	columns := []string{}
	for k := range jsonMap {
		if !slices.Contains(t.PrimaryKeyCmp(), qb.Eq(k)) {
			columns = append(columns, k)
		} else {
			slog.Debug("Column is part of primary key, excluding from columns slice", slog.String("pKeyColumn", k))
		}
	}
	return t.UpdateQuery(s, columns...).BindStruct(item).ExecRelease()
}
func (c *CassandraAdapter) Delete(item any, filter map[string]any) error {
	s, err := c.createSession()
	if err != nil {
		return err
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(item)
	if tableErr != nil {
		return tableErr
	}
	return t.DeleteQuery(s).BindStruct(item).ExecRelease()
}
func (c *CassandraAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string) (string, error) {
	s, err := c.createSession()
	if err != nil {
		return "", err
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(dest)
	if tableErr != nil {
		return "", tableErr
	}
	q := t.SelectQuery(s)
	if cursor != "" {
		bytes, err := base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			return "", fmt.Errorf("invalid cursor: %w", err)
		}
		q = q.PageState(bytes)
	}
	if limit > 0 {
		q = q.PageSize(limit)
	}
	// TODO: Verify pagination behavior
	return cursor, q.SelectRelease(dest)
}
func (c *CassandraAdapter) Search(dest any, sortKey string, query string, limit int, cursor string) (string, error) {
	return "", fmt.Errorf("unimplemented")
}
func (c *CassandraAdapter) Count(dest any) (int64, error) {
	s, err := c.createSession()
	if err != nil {
		return -1, err
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(dest)
	if tableErr != nil {
		return -1, tableErr
	}
	return -1, t.SelectBuilder().CountAll().Query(s).BindStruct(dest).ExecRelease()
}
func (c *CassandraAdapter) Query(dest any, statement string, limit int, cursor string) (string, error) {
	return "", fmt.Errorf("unimplemented")
}
