package storage

import (
	"reflect"
	"testing"
)

func TestGetSQLAdapterInstance(t *testing.T) {
	type args struct {
		config map[string]string
	}
	tests := []struct {
		name string
		args args
		want *SQLAdapter
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetSQLAdapterInstance(tt.args.config); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetSQLAdapterInstance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLAdapter_OpenConnection(t *testing.T) {
	tests := []struct {
		name string
		s    *SQLAdapter
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.s.OpenConnection()
		})
	}
}

func TestSQLAdapter_Execute(t *testing.T) {
	type args struct {
		statement string
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Execute(tt.args.statement); (err != nil) != tt.wantErr {
				t.Errorf("SQLAdapter.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLAdapter_Ping(t *testing.T) {
	tests := []struct {
		name    string
		s       *SQLAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Ping(); (err != nil) != tt.wantErr {
				t.Errorf("SQLAdapter.Ping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLAdapter_GetType(t *testing.T) {
	tests := []struct {
		name string
		s    *SQLAdapter
		want StorageAdapterType
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetType(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SQLAdapter.GetType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLAdapter_GetProvider(t *testing.T) {
	tests := []struct {
		name string
		s    *SQLAdapter
		want StorageProviders
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetProvider(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SQLAdapter.GetProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLAdapter_GetSchemaName(t *testing.T) {
	tests := []struct {
		name string
		s    *SQLAdapter
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetSchemaName(); got != tt.want {
				t.Errorf("SQLAdapter.GetSchemaName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLAdapter_CreateSchema(t *testing.T) {
	tests := []struct {
		name    string
		s       *SQLAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.CreateSchema(); (err != nil) != tt.wantErr {
				t.Errorf("SQLAdapter.CreateSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLAdapter_CreateMigrationTable(t *testing.T) {
	tests := []struct {
		name    string
		s       *SQLAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.CreateMigrationTable(); (err != nil) != tt.wantErr {
				t.Errorf("SQLAdapter.CreateMigrationTable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLAdapter_UpdateMigrationTable(t *testing.T) {
	type args struct {
		id   int
		name string
		desc string
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.UpdateMigrationTable(tt.args.id, tt.args.name, tt.args.desc); (err != nil) != tt.wantErr {
				t.Errorf("SQLAdapter.UpdateMigrationTable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLAdapter_GetLatestMigration(t *testing.T) {
	tests := []struct {
		name    string
		s       *SQLAdapter
		want    int
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.GetLatestMigration()
			if (err != nil) != tt.wantErr {
				t.Fatalf("SQLAdapter.GetLatestMigration() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("SQLAdapter.GetLatestMigration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLAdapter_Create(t *testing.T) {
	type args struct {
		item   any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Create(tt.args.item, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("SQLAdapter.Create() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLAdapter_Get(t *testing.T) {
	type args struct {
		dest   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Get(tt.args.dest, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("SQLAdapter.Get() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLAdapter_Update(t *testing.T) {
	type args struct {
		item   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Update(tt.args.item, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("SQLAdapter.Update() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLAdapter_Delete(t *testing.T) {
	type args struct {
		item   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Delete(tt.args.item, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("SQLAdapter.Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSQLAdapter_executePaginatedQuery(t *testing.T) {
	type args struct {
		dest    any
		sortKey string
		limit   int
		cursor  string
		builder queryBuilder
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		want    string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.executePaginatedQuery(tt.args.dest, tt.args.sortKey, tt.args.limit, tt.args.cursor, tt.args.builder)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SQLAdapter.executePaginatedQuery() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("SQLAdapter.executePaginatedQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLAdapter_List(t *testing.T) {
	type args struct {
		dest    any
		sortKey string
		filter  map[string]any
		limit   int
		cursor  string
		params  []map[string]any
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		want    string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.List(tt.args.dest, tt.args.sortKey, tt.args.filter, tt.args.limit, tt.args.cursor, tt.args.params...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SQLAdapter.List() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("SQLAdapter.List() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLAdapter_Search(t *testing.T) {
	type args struct {
		dest    any
		sortKey string
		query   string
		limit   int
		cursor  string
		params  []map[string]any
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		want    string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.Search(tt.args.dest, tt.args.sortKey, tt.args.query, tt.args.limit, tt.args.cursor, tt.args.params...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SQLAdapter.Search() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("SQLAdapter.Search() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLAdapter_Count(t *testing.T) {
	type args struct {
		dest   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		want    int64
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.Count(tt.args.dest, tt.args.filter, tt.args.params...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SQLAdapter.Count() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("SQLAdapter.Count() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLAdapter_Query(t *testing.T) {
	type args struct {
		dest      any
		statement string
		limit     int
		cursor    string
		params    []map[string]any
	}
	tests := []struct {
		name    string
		s       *SQLAdapter
		args    args
		want    string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.Query(tt.args.dest, tt.args.statement, tt.args.limit, tt.args.cursor, tt.args.params...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SQLAdapter.Query() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("SQLAdapter.Query() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSQLAdapter_buildQuery(t *testing.T) {
	type args struct {
		filter map[string]any
	}
	tests := []struct {
		name string
		s    *SQLAdapter
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.buildQuery(tt.args.filter); got != tt.want {
				t.Errorf("SQLAdapter.buildQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}
