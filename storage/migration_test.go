package storage

import (
	"reflect"
	"testing"
)

func TestNewDatabaseMigration(t *testing.T) {
	type args struct {
		storageAdapter StorageAdapter
	}
	tests := []struct {
		name string
		args args
		want *DatabaseMigration
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewDatabaseMigration(tt.args.storageAdapter); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewDatabaseMigration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDatabaseMigration_getMigrationFiles(t *testing.T) {
	tests := []struct {
		name    string
		m       *DatabaseMigration
		want    map[string]MigrationFile
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.m.getMigrationFiles()
			if (err != nil) != tt.wantErr {
				t.Fatalf("DatabaseMigration.getMigrationFiles() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DatabaseMigration.getMigrationFiles() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDatabaseMigration_rollbackMigration(t *testing.T) {
	type args struct {
		migration MigrationFile
	}
	tests := []struct {
		name    string
		m       *DatabaseMigration
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.m.rollbackMigration(tt.args.migration); (err != nil) != tt.wantErr {
				t.Errorf("DatabaseMigration.rollbackMigration() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDatabaseMigration_runMigrations(t *testing.T) {
	type args struct {
		migrations map[string]MigrationFile
	}
	tests := []struct {
		name string
		m    *DatabaseMigration
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.runMigrations(tt.args.migrations)
		})
	}
}

func TestDatabaseMigration_Migrate(t *testing.T) {
	tests := []struct {
		name string
		m    *DatabaseMigration
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.m.Migrate()
		})
	}
}
