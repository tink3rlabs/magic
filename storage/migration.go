package storage

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/tink3rlabs/magic/logger"

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
	storageType     StorageAdapterType
	storageProvider StorageProviders
	storage         StorageAdapter
}

func NewDatabaseMigration(storageAdapter StorageAdapter) *DatabaseMigration {
	m := DatabaseMigration{
		storage:         storageAdapter,
		storageType:     storageAdapter.GetType(),
		storageProvider: storageAdapter.GetProvider(),
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
	latestMigrationId, err := m.storage.GetLatestMigration()
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
			err = m.storage.UpdateMigrationTable(migrationId, k, mf.Description)
			if err != nil {
				logger.Fatal("failed to update migration table", slog.Any("error", err))
			}
		}
	}
}

func (m *DatabaseMigration) Migrate() {
	if m.storageType == DYNAMODB {
		slog.Info(fmt.Sprintf(`using %s storage adapter, migrations are not supported`, m.storageType))
	} else {
		slog.Info(fmt.Sprintf(`using %s storage adapter, executing migrations`, m.storageType))
		migrations, err := m.getMigrationFiles()
		if err != nil {
			logger.Fatal("failed to get migration files", slog.Any("error", err))
		}
		slog.Info("creating schema")
		err = m.storage.CreateSchema()
		if err != nil {
			logger.Fatal("failed to create schema", slog.Any("error", err))
		}
		slog.Info("creating migration table")
		err = m.storage.CreateMigrationTable()
		if err != nil {
			logger.Fatal("failed to create migration table", slog.Any("error", err))
		}
		m.runMigrations(migrations)
		slog.Info("finished running migrations")
	}
}
