package storage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

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
	hosts           = "hosts"
	keyspace        = "keyspace"
	tables          = "tables"
	provider        = "provider"
	username        = "username"
	password        = "password"
	protocolVersion = "protocolVersion"
	port            = "port"
)

type CassandraAdapter struct {
	StorageAdapter
	config        map[string]string
	clusterConfig *gocql.ClusterConfig
	provider      StorageProviders
	tables        map[string]*table.Table
}

func GetCassandraAdapter(config map[string]string) (*CassandraAdapter, error) {
	if cassandraAdapterInstance == nil {
		cassandraAdapterLock.Lock()
		defer cassandraAdapterLock.Unlock()
		if cassandraAdapterInstance == nil {
			cassandraAdapterInstance = &CassandraAdapter{config: config}
			cassandraAdapterInstance.initConfig()
			// The call to createSchema will set clusterConfig.Keyspace to the
			// actual Keyspace, this is why its here.
			if createSchemaErr := cassandraAdapterInstance.CreateSchema(); createSchemaErr != nil {
				errMessage := "failed to call CreateSchema"
				slog.Error(errMessage, slog.Any("error", createSchemaErr))
				return nil, errors.Join(fmt.Errorf("%s", errMessage), createSchemaErr)
			}
			if err := cassandraAdapterInstance.initializeTableMappers(); err != nil {
				errMessage := "failed to call initializeTableMappers"
				slog.Error(errMessage, slog.Any("error", err))
				return nil, errors.Join(fmt.Errorf("%s", errMessage), err)
			}
		}
	}
	return cassandraAdapterInstance, nil

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
	c.clusterConfig = gocql.NewCluster(strings.Split(c.config[hosts], ",")...)

	/**
	 * Setting to "system" to handle the case where the actual
	 * Keyspace does not exist yet, so createSchema() will create it, then
	 * set the actual Keyspace in c.clusterConfig
	 */
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
	s, e := c.createSession()
	if e != nil {
		return errors.Join(
			fmt.Errorf("failed creating a session"),
			e,
		)
	}
	metadata, mErr := s.KeyspaceMetadata(c.config[keyspace])
	if mErr != nil {
		return errors.Join(
			fmt.Errorf("failed reading tables metadata"),
			mErr,
		)
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
	itemName := typeName(item)
	tableName := reflectx.CamelToSnakeASCII(itemName)
	if t, ok := c.tables[tableName]; !ok {
		return nil, fmt.Errorf("no table metadata for [%s]", tableName)
	} else {
		return t, nil
	}
}

func (c *CassandraAdapter) Execute(statement string) error {
	if s, err := c.createSession(); err != nil {
		return errors.Join(
			fmt.Errorf("failed creating a session"),
			err,
		)
	} else {
		defer s.Close()
		return s.ExecStmt(statement)
	}
}

func (c *CassandraAdapter) Ping() error {
	if s, err := c.createSession(); err != nil {
		return errors.Join(
			fmt.Errorf("failed creating a session to %v", c.clusterConfig.Hosts),
			err,
		)
	} else {
		defer s.Close()
		return nil
	}
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
			"CREATE KEYSPACE IF NOT EXISTS %s WITH REPLICATION = {'class':'%s', 'replication_factor': %d}",
			c.GetSchemaName(),
			replicationClass,
			replicationFactor,
		),
	)
	if createKeyspaceErr != nil {
		return errors.Join(
			fmt.Errorf("failed creating keyspace %s", c.GetSchemaName()),
			createKeyspaceErr,
		)
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
		return errors.Join(
			fmt.Errorf("failed getting migration table"),
			e,
		)
	}
	s, sErr := c.createSession()
	if sErr != nil {
		return errors.Join(
			fmt.Errorf("failed creating a session"),
			sErr,
		)
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
		return -1, errors.Join(
			fmt.Errorf("failed getting migration table"),
			e,
		)
	}
	s, sErr := c.createSession()
	if sErr != nil {
		return -1, errors.Join(
			fmt.Errorf("failed creating a session"),
			e,
		)
	}
	var latestMigration int
	migrationErr := t.SelectBuilder("id").Max("id").Query(s).Bind(&latestMigration).ExecRelease()
	if migrationErr != nil {
		return -1, errors.Join(
			fmt.Errorf("failed getLatestMigration"),
			migrationErr,
		)
	}
	return latestMigration, nil
}

func (c *CassandraAdapter) Create(item any, params ...map[string]any) error {
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
func (c *CassandraAdapter) Get(dest any, filters map[string]any, params ...map[string]any) error {
	s, sessionErr := c.createSession()
	if sessionErr != nil {
		return errors.Join(fmt.Errorf("get failed to create session"), sessionErr)
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(dest)
	if tableErr != nil {
		return errors.Join(fmt.Errorf("get failed getting table for item"), tableErr)
	}
	return t.GetQuery(s).BindMap(filters).GetRelease(dest)
}
func (c *CassandraAdapter) Update(item any, filters map[string]any, params ...map[string]any) error {
	s, sessionErr := c.createSession()
	if sessionErr != nil {
		return errors.Join(fmt.Errorf("update failed to create session"), sessionErr)
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(item)
	if tableErr != nil {
		return errors.Join(fmt.Errorf("update failed getting table for item"), tableErr)
	}

	marshalledItem, marshalErr := json.Marshal(item)
	if marshalErr != nil {
		return errors.Join(fmt.Errorf("updated failed json.Marshal"), marshalErr)
	}
	var jsonMap map[string]any
	unmarshalErr := json.Unmarshal(marshalledItem, &jsonMap)
	if unmarshalErr != nil {
		return errors.Join(fmt.Errorf("update failed json.Unmarshal"), unmarshalErr)
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
func (c *CassandraAdapter) Delete(item any, filters map[string]any, params ...map[string]any) error {
	s, sessionErr := c.createSession()
	if sessionErr != nil {
		return errors.Join(fmt.Errorf("delete failed to create session"), sessionErr)
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(item)
	if tableErr != nil {
		return errors.Join(fmt.Errorf("delete failed getting table for item"), tableErr)
	}
	return t.DeleteQuery(s).BindMap(filters).ExecRelease()
}
func (c *CassandraAdapter) List(dest any, sortKey string, filters map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	s, sessionErr := c.createSession()
	if sessionErr != nil {
		return "", errors.Join(fmt.Errorf("list failed to create session"), sessionErr)
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(dest)
	if tableErr != nil {
		return "", errors.Join(fmt.Errorf("list failed getting table for item"), tableErr)
	}
	q := t.SelectQuery(s).BindMap(filters)
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
func (c *CassandraAdapter) Search(dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
	return "", fmt.Errorf("unimplemented")
}
func (c *CassandraAdapter) Count(dest any, filter map[string]any, params ...map[string]any) (int64, error) {
	s, sessionErr := c.createSession()
	if sessionErr != nil {
		return -1, errors.Join(fmt.Errorf("count failed to create session"), sessionErr)
	}
	defer s.Close()

	t, tableErr := c.getTableForItem(dest)
	if tableErr != nil {
		return -1, errors.Join(fmt.Errorf("count failed getting table for item"), tableErr)
	}
	return -1, t.SelectBuilder().CountAll().Query(s).BindStruct(dest).ExecRelease()
}
func (c *CassandraAdapter) Query(dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
	return "", fmt.Errorf("unimplemented")
}
