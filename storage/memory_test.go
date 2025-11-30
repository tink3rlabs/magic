package storage

import (
	"reflect"
	"testing"
)

func TestGetMemoryAdapterInstance(t *testing.T) {
	tests := []struct {
		name string
		want *MemoryAdapter
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetMemoryAdapterInstance(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetMemoryAdapterInstance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryAdapter_Execute(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name    string
		m       *MemoryAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.m.Execute(tt.args.s); (err != nil) != tt.wantErr {
				t.Errorf("MemoryAdapter.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryAdapter_Ping(t *testing.T) {
	tests := []struct {
		name    string
		m       *MemoryAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.m.Ping(); (err != nil) != tt.wantErr {
				t.Errorf("MemoryAdapter.Ping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryAdapter_GetType(t *testing.T) {
	tests := []struct {
		name string
		m    *MemoryAdapter
		want StorageAdapterType
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.GetType(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MemoryAdapter.GetType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryAdapter_GetProvider(t *testing.T) {
	tests := []struct {
		name string
		m    *MemoryAdapter
		want StorageProviders
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.GetProvider(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MemoryAdapter.GetProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryAdapter_GetSchemaName(t *testing.T) {
	tests := []struct {
		name string
		m    *MemoryAdapter
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.GetSchemaName(); got != tt.want {
				t.Errorf("MemoryAdapter.GetSchemaName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryAdapter_CreateSchema(t *testing.T) {
	tests := []struct {
		name    string
		m       *MemoryAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.m.CreateSchema(); (err != nil) != tt.wantErr {
				t.Errorf("MemoryAdapter.CreateSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryAdapter_CreateMigrationTable(t *testing.T) {
	tests := []struct {
		name    string
		m       *MemoryAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.m.CreateMigrationTable(); (err != nil) != tt.wantErr {
				t.Errorf("MemoryAdapter.CreateMigrationTable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryAdapter_UpdateMigrationTable(t *testing.T) {
	type args struct {
		id   int
		name string
		desc string
	}
	tests := []struct {
		name    string
		m       *MemoryAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.m.UpdateMigrationTable(tt.args.id, tt.args.name, tt.args.desc); (err != nil) != tt.wantErr {
				t.Errorf("MemoryAdapter.UpdateMigrationTable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryAdapter_GetLatestMigration(t *testing.T) {
	tests := []struct {
		name    string
		m       *MemoryAdapter
		want    int
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.m.GetLatestMigration()
			if (err != nil) != tt.wantErr {
				t.Fatalf("MemoryAdapter.GetLatestMigration() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("MemoryAdapter.GetLatestMigration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryAdapter_Create(t *testing.T) {
	type args struct {
		item   any
		params []map[string]any
	}
	tests := []struct {
		name    string
		m       *MemoryAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.m.Create(tt.args.item, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("MemoryAdapter.Create() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryAdapter_Get(t *testing.T) {
	type args struct {
		dest   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		m       *MemoryAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.m.Get(tt.args.dest, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("MemoryAdapter.Get() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryAdapter_Update(t *testing.T) {
	type args struct {
		item   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		m       *MemoryAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.m.Update(tt.args.item, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("MemoryAdapter.Update() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryAdapter_Delete(t *testing.T) {
	type args struct {
		item   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		m       *MemoryAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.m.Delete(tt.args.item, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("MemoryAdapter.Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryAdapter_List(t *testing.T) {
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
		m       *MemoryAdapter
		args    args
		want    string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.m.List(tt.args.dest, tt.args.sortKey, tt.args.filter, tt.args.limit, tt.args.cursor, tt.args.params...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MemoryAdapter.List() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("MemoryAdapter.List() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryAdapter_Search(t *testing.T) {
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
		m       *MemoryAdapter
		args    args
		want    string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.m.Search(tt.args.dest, tt.args.sortKey, tt.args.query, tt.args.limit, tt.args.cursor, tt.args.params...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MemoryAdapter.Search() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("MemoryAdapter.Search() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryAdapter_Count(t *testing.T) {
	type args struct {
		dest   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		m       *MemoryAdapter
		args    args
		want    int64
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.m.Count(tt.args.dest, tt.args.filter, tt.args.params...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MemoryAdapter.Count() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("MemoryAdapter.Count() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryAdapter_Query(t *testing.T) {
	type args struct {
		dest      any
		statement string
		limit     int
		cursor    string
		params    []map[string]any
	}
	tests := []struct {
		name    string
		m       *MemoryAdapter
		args    args
		want    string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.m.Query(tt.args.dest, tt.args.statement, tt.args.limit, tt.args.cursor, tt.args.params...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MemoryAdapter.Query() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("MemoryAdapter.Query() = %v, want %v", got, tt.want)
			}
		})
	}
}
