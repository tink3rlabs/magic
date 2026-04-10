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
	"github.com/scylladb/gocqlx/v2"
	"github.com/scylladb/gocqlx/v2/qb"
	"github.com/scylladb/gocqlx/v2/table"
)

var (
	cassandraAdapterLock = &sync.Mutex{}
	instance             *CassandraAdapter
)

const (
	hosts           = "hosts"
	keyspace        = "keyspace"
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
	session       gocqlx.Session
}

func GetCassandraAdapter(config map[string]string) (*CassandraAdapter, error) {
	if instance == nil {
		cassandraAdapterLock.Lock()
		defer cassandraAdapterLock.Unlock()
		if instance == nil {
			instance = &CassandraAdapter{config: config}
			err := instance.initConfig()
			if err != nil {
				return nil, fmt.Errorf("initConfig failed: %w", err)
			}
			if session, err := instance.createSession(); err != nil {
				return nil, fmt.Errorf("failed to create session: %w", err)
			} else {
				instance.session = session
			}

			// The call to createSchema will set clusterConfig.Keyspace to the
			// actual Keyspace, this is why its here.
			if createSchemaErr := instance.CreateSchema(); createSchemaErr != nil {
				return nil, fmt.Errorf("failed to create schema: %w", createSchemaErr)
			}
			if err := instance.initializeTableMappers(); err != nil {
				return nil, fmt.Errorf("failed to initialize table mappers: %w", err)
			}
		}
	}
	return instance, nil

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

func (c *CassandraAdapter) initConfig() error {
	c.clusterConfig = gocql.NewCluster(strings.Split(c.config[hosts], ",")...)

	// Setting to "system" to handle the case where the actual
	// Keyspace does not exist yet, so createSchema() will create it, then
	// set the actual Keyspace in c.clusterConfig
	c.clusterConfig.Keyspace = "system"
	if username, ok := c.config[username]; ok {
		if password, ok := c.config[password]; !ok {
			return fmt.Errorf("password is required when username is provided")
		} else {
			c.clusterConfig.Authenticator = gocql.PasswordAuthenticator{
				Username: username,
				Password: c.config[password],
			}
		}
	}
	if protoVersion, ok := c.config[protocolVersion]; ok {
		parsedProtoVer, parseErr := strconv.Atoi(protoVersion)
		if parseErr != nil {
			return fmt.Errorf("failed to parse protocolVersion %s: %w", protoVersion, parseErr)
		}
		c.clusterConfig.ProtoVersion = parsedProtoVer
	}
	if portStr, ok := c.config[port]; ok {
		parsedPort, parseErr := strconv.Atoi(portStr)
		if parseErr != nil {
			return fmt.Errorf("failed to parse port %s: %w", portStr, parseErr)
		}
		c.clusterConfig.Port = parsedPort
	}
	return nil
}

func (c *CassandraAdapter) createSession() (gocqlx.Session, error) {
	return gocqlx.WrapSession(c.clusterConfig.CreateSession())
}

func (c *CassandraAdapter) initializeTableMappers() error {
	metadata, mErr := c.session.KeyspaceMetadata(c.config[keyspace])
	if mErr != nil {
		return fmt.Errorf("failed reading tables metadata: %w", mErr)
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
	execErr := c.session.ExecStmt(statement)
	if execErr != nil {
		return fmt.Errorf("failed executing statement %s: %w", statement, execErr)
	}
	return nil
}

func (c *CassandraAdapter) Ping() error {
	return c.session.Query("SELECT now() FROM system.local", []string{}).ExecRelease()
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
		return fmt.Errorf("failed creating keyspace %s: %w", c.GetSchemaName(), createKeyspaceErr)
	}
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
		return fmt.Errorf("failed getting migration table: %w", e)
	}
	params := map[string]any{
		"id":          id,
		"name":        name,
		"description": desc,
		"timestamp":   time.Now().UnixMilli(),
	}
	q := t.InsertQuery(c.session).BindMap(params)
	err := q.ExecRelease()
	if err != nil {
		return fmt.Errorf("failed updating migration table: %w", err)
	}
	return nil
}

func (c *CassandraAdapter) GetLatestMigration() (int, error) {
	t, exist := c.tables["migrations"]
	if !exist {
		return -1, errors.New("failed getting migration table")
	}
	var latestMigration int
	err := t.SelectBuilder("id").
		Max("id").
		Query(c.session).
		Bind(&latestMigration).
		ExecRelease()
	if err != nil {
		return -1,
			fmt.Errorf("failed getLatestMigration: %w", err)
	}
	return latestMigration, nil
}

func (c *CassandraAdapter) Create(item any, params ...map[string]any) error {
	t, tableErr := c.getTableForItem(item)
	if tableErr != nil {
		return fmt.Errorf("failed to get table for item: %w", tableErr)
	}
	return t.InsertQuery(c.session).BindStruct(item).ExecRelease()
}

func (c *CassandraAdapter) Get(dest any, filters map[string]any, params ...map[string]any) error {
	t, tableErr := c.getTableForItem(dest)
	if tableErr != nil {
		return fmt.Errorf("failed getting table for item: %w", tableErr)
	}
	return t.GetQuery(c.session).BindMap(filters).GetRelease(dest)
}

func (c *CassandraAdapter) Update(item any, filters map[string]any, params ...map[string]any) error {
	t, tableErr := c.getTableForItem(item)
	if tableErr != nil {
		return fmt.Errorf("failed getting table for item: %w", tableErr)
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
	err := t.UpdateQuery(c.session, columns...).BindStruct(item).ExecRelease()
	if err != nil {
		return fmt.Errorf("failed updating item: %w", err)
	}
	return nil
}

func (c *CassandraAdapter) Delete(item any, filters map[string]any, params ...map[string]any) error {
	t, tableErr := c.getTableForItem(item)
	if tableErr != nil {
		return fmt.Errorf("failed getting table for item: %w", tableErr)
	}

	err := t.DeleteQuery(c.session).BindMap(filters).ExecRelease()
	if err != nil {
		return fmt.Errorf("failed deleting item: %w", err)
	}
	return nil
}

func (c *CassandraAdapter) List(dest any, sortKey string, filters map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	t, tableErr := c.getTableForItem(dest)
	if tableErr != nil {
		return "", fmt.Errorf("list failed getting table for item: %w", tableErr)
	}
	q := t.SelectQuery(c.session).BindMap(filters)
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
	err := q.SelectRelease(dest)
	if err != nil {
		return "", fmt.Errorf("list failed selecting release: %w", err)
	}
	return cursor, nil
}

func (c *CassandraAdapter) Search(dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
	return "", fmt.Errorf("unimplemented")
}

func (c *CassandraAdapter) Count(dest any, filter map[string]any, params ...map[string]any) (int64, error) {
	t, tableErr := c.getTableForItem(dest)
	if tableErr != nil {
		return -1, fmt.Errorf("count failed getting table for item: %w", tableErr)
	}
	var count int64
	err := t.SelectBuilder().CountAll().Query(c.session).GetRelease(count)
	if err != nil {
		return -1, fmt.Errorf("count failed executing count query: %w", err)
	}
	return count, nil
}

func (c *CassandraAdapter) Query(dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
	return "", fmt.Errorf("unimplemented")
}
