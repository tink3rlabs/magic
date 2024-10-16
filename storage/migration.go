package storage

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tink3rlabs/magic/logger"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type MigrationFile struct {
	Description string
	Migrations  []Migration
}

type Migration struct {
	Migrate  string
	Rollback string
}

type DatabaseMigration struct {
	storageType     string
	storageProvider string
	storage         StorageAdapter
}

func NewDatabaseMigration() *DatabaseMigration {
	s := StorageAdapterFactory{}
	storageAdapter, err := s.GetInstance(DEFAULT)
	if err != nil {
		logger.Fatal("failed to create DatabaseMigration instance", slog.Any("error", err.Error()))
	}
	m := DatabaseMigration{
		storage:         storageAdapter,
		storageType:     viper.GetString("storage.type"),
		storageProvider: viper.GetString("storage.provider"),
	}
	return &m
}

func (m *DatabaseMigration) getMigrationFiles() (map[string]MigrationFile, error) {
	var err error
	migrations := map[string]MigrationFile{}
	path := fmt.Sprintf("config/migrations/%s", m.storageProvider)
	files, _ := ConfigFs.ReadDir(path)

	for _, f := range files {
		var contents []byte
		contents, err = ConfigFs.ReadFile(filepath.Join(path, f.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %v", f.Name(), err)
		}
		mf := MigrationFile{}
		err = yaml.Unmarshal([]byte(contents), &mf)
		if err != nil {
			return nil, fmt.Errorf("failed to parse migration file %s: %v", f.Name(), err)
		}
		migrations[f.Name()] = mf
	}
	return migrations, nil
}

func (m *DatabaseMigration) createSchema() error {
	if m.storageProvider != string(SQLITE) {
		statement := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", viper.GetString("storage.config.schema"))
		return m.storage.Execute(statement)
	}
	return nil
}

func (m *DatabaseMigration) createMigrationTable() error {
	var statement string
	switch m.storageProvider {
	case string(POSTGRESQL):
		statement = "CREATE TABLE IF NOT EXISTS migrations (id NUMERIC PRIMARY KEY, name TEXT, description TEXT, timestamp NUMERIC)"
	case string(MYSQL):
		statement = "CREATE TABLE IF NOT EXISTS migrations (id INT PRIMARY KEY, name TEXT, description TEXT, timestamp BIGINT)"
	case string(SQLITE):
		statement = "CREATE TABLE IF NOT EXISTS migrations (id INTEGER PRIMARY KEY, name TEXT, description TEXT, timestamp INTEGER)"
	}
	return m.storage.Execute(statement)
}

func (m *DatabaseMigration) updateMigrationTable(id int, name string, desc string) error {
	statement := fmt.Sprintf(`INSERT INTO migrations VALUES(%v, '%v', '%v', %v)`, id, name, desc, time.Now().UnixMilli())
	return m.storage.Execute(statement)
}

func (m *DatabaseMigration) getLatestMigration() (int, error) {
	var statement string
	var latestMigration int
	switch m.storageType {
	case string(SQL):
		statement = "SELECT max(id) from migrations"
		a := GetSQLAdapterInstance()
		result := a.DB.Raw(statement).Scan(&latestMigration)
		if result.Error != nil {
			//either a real issue or there are no migrations yet check if we can query the migration table
			var count int
			statement = "SELECT count(*) from migrations"
			a := GetSQLAdapterInstance()
			countResult := a.DB.Raw(statement).Scan(&count)
			if countResult.Error != nil {
				return latestMigration, result.Error
			}
		}
	}
	return latestMigration, nil
}

func (m *DatabaseMigration) rollbackMigration(migration MigrationFile) error {
	var err error
	slices.Reverse(migration.Migrations)
	for _, s := range migration.Migrations {
		err = m.storage.Execute(s.Rollback)
		if err != nil {
			break
		}
	}
	return err
}

func (m *DatabaseMigration) runMigrations(migrations map[string]MigrationFile) {
	slog.Info("Getting last migration applied")
	rollback := false
	latestMigrationId, err := m.getLatestMigration()
	if err != nil {
		logger.Fatal("failed to get latest migration", slog.Any("error", err))
	}

	//iterating over a map is randomized so we need to make sure we use the correct order of migrations
	keys := make([]string, 0, len(migrations))
	for k := range migrations {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		migrationId, err := strconv.Atoi(strings.Split(k, "__")[0])
		if err != nil {
			logger.Fatal("failed to determine migration id", slog.Any("error", err))
		}
		if migrationId > latestMigrationId {
			mf := migrations[k]
			for _, stmt := range mf.Migrations {
				err := m.storage.Execute(stmt.Migrate)
				if err != nil {
					slog.Error("failed to execute migration statement", slog.Any("error", err))
					slog.Info("failed to execute migration statement", slog.String("key", k))
					rollback = true
					err = m.rollbackMigration(mf)
					if err != nil {
						logger.Fatal("failed to rollback migration", slog.Any("error", err))
					}
					slog.Info("rollback successful")
					break
				}
			}
			if rollback {
				break
			}
			slog.Info("updating migration table for", slog.String("key", k))
			err = m.updateMigrationTable(migrationId, k, mf.Description)
			if err != nil {
				logger.Fatal("failed to update migration table", slog.Any("error", err))
			}
		}
	}
}

func (m *DatabaseMigration) Migrate() {
	if (m.storageType == string(MEMORY)) || (m.storageType == string(DYNAMODB)) {
		slog.Info(fmt.Sprintf(`using %s storage adapter, migrations are not supported`, m.storageType))
	} else {
		slog.Info("using a persistent storage adapter, executing migrations")
		migrations, err := m.getMigrationFiles()
		if err != nil {
			logger.Fatal("failed to get migration files", slog.Any("error", err))
		}
		slog.Info("creating schema")
		err = m.createSchema()
		if err != nil {
			logger.Fatal("failed to create schema", slog.Any("error", err))
		}
		slog.Info("creating migration table")
		err = m.createMigrationTable()
		if err != nil {
			logger.Fatal("failed to create migration table", slog.Any("error", err))
		}
		m.runMigrations(migrations)
		slog.Info("finished running migrations")
	}
}
