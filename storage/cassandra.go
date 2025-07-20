package storage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
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
	"github.com/tink3rlabs/magic/logger"
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
	username            = "username"
	password            = "password"
	protocolVersion     = "protocolVersion"
	port                = "port"
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
			cassandraAdapterInstance.initConfig()
			// The call to createSchema will set clusterConfig.Keyspace to the
			// actual Keyspace, this is why its here.
			if createSchemaErr := cassandraAdapterInstance.CreateSchema(); createSchemaErr != nil {
				slog.Error("failed to call CreateSchema", slog.Any("error", createSchemaErr))
				return nil
			}
			if err := cassandraAdapterInstance.initializeTableMappers(); err != nil {
				slog.Error("failed to call initializeTableMappers", slog.Any("error", err))
				return nil
			}
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
	return CASSANDRA
}

func (c *CassandraAdapter) initConfig() {
	c.clusterConfig = &gocql.ClusterConfig{}
	c.clusterConfig.Hosts = strings.Split(c.config[hosts], ",")
	// Setting to "system" to handle the case where the actual
	// Keyspace does not exist yet, so createSchema() will create it, then
	// set the actual Keyspace in c.clusterConfig
	c.clusterConfig.Keyspace = "system"
	if username, ok := c.config[username]; ok {
		c.clusterConfig.Authenticator = gocql.PasswordAuthenticator{
			Username: username,
			Password: c.config[password],
		}
	}
	if protoVersion, ok := c.config[protocolVersion]; ok {
		parsedProtoVer, parseErr := strconv.Atoi(protoVersion)
		if parseErr != nil {
			logger.Fatal("failed to parse protocol version %s", protoVersion)
		}
		c.clusterConfig.ProtoVersion = parsedProtoVer
	}
	if portStr, ok := c.config[port]; ok {
		parsedPort, parseErr := strconv.Atoi(portStr)
		if parseErr != nil {
			logger.Fatal("failed to parse port %s", portStr)
		}
		c.clusterConfig.Port = parsedPort
	}
}

func (c *CassandraAdapter) createSession() (gocqlx.Session, error) {
	return gocqlx.WrapSession(c.clusterConfig.CreateSession())
}

func (c *CassandraAdapter) initializeTableMappers() error {
	autoDiscovery, parseErr := strconv.ParseBool(c.config[tablesAutoDiscovery])
	if parseErr != nil {
		return parseErr
	}
	if !autoDiscovery {
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

// CreateSchema is a function that will create the application Cassandra Keyspace
// if it doesn't exist.
// To handle the initial/first run of the app, when the Keyspace hasn't been created
// The clusterConfig.Keyspace is set to 'system' during initConfig function call
// If CreateSchema "CREATE KEYSPACE" query succeeded, clusterConfig.Keyspace will be
// set to the actual Keyspace name from the c.config .
func (c *CassandraAdapter) CreateSchema() error {
	replicationClass := "SimpleStrategy"
	replicationFactor := 1
	createKeyspaceErr := c.Execute(
		fmt.Sprintf(
			"CREATE KEYSPACE IF NOT EXIST %s WITH REPLICATION = {'class':'%s', 'replication_factor': %d}",
			c.GetSchemaName(),
			replicationClass,
			replicationFactor,
		),
	)
	if createKeyspaceErr != nil {
		return errors.Join(fmt.Errorf("failed creating keyspace %s", c.GetSchemaName()), createKeyspaceErr)
	}
	slog.Debug("schema created, setting clusterConfig.Keyspace",
		slog.String("keyspace", c.GetSchemaName()),
	)
	c.clusterConfig.Keyspace = c.config[keyspace]
	return nil
}

func (c *CassandraAdapter) CreateMigrationTable() error {
	statement := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s.migrations (
			id DECIMAL PRIMARY KEY,
			name TEXT,
			description TEXT,
			timestamp DECIMAL)`,
		c.GetSchemaName())
	return c.Execute(statement)

}

func (c *CassandraAdapter) UpdateMigrationTable(id int, name string, desc string) error {
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

func (c *CassandraAdapter) GetLatestMigration() (int, error) {
	t, e := c.getTableForItem("migrations")
	if e != nil {
		return -1, e
	}
	s, sErr := c.createSession()
	if sErr != nil {
		return -1, sErr
	}
	var latestMigration int
	migrationErr := t.SelectBuilder("id").Max("id").Query(s).Bind(&latestMigration).ExecRelease()
	if migrationErr != nil {
		return -1, fmt.Errorf("failed getLatestMigration: %w", migrationErr)
	}
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
		return fmt.Errorf("failed json.Marshal: %w", marshalErr)
	}
	var jsonMap map[string]any
	unmarshalErr := json.Unmarshal(marshalledItem, &jsonMap)
	if unmarshalErr != nil {
		return fmt.Errorf("failed json.Unmarshal: %w", unmarshalErr)
	}
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
