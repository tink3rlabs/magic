package storage

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestGetDynamoDBAdapterInstance(t *testing.T) {
	type args struct {
		config map[string]string
	}
	tests := []struct {
		name string
		args args
		want *DynamoDBAdapter
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetDynamoDBAdapterInstance(tt.args.config); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetDynamoDBAdapterInstance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_OpenConnection(t *testing.T) {
	tests := []struct {
		name string
		s    *DynamoDBAdapter
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.s.OpenConnection()
		})
	}
}

func TestDynamoDBAdapter_Execute(t *testing.T) {
	type args struct {
		statement string
	}
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Execute(tt.args.statement); (err != nil) != tt.wantErr {
				t.Errorf("DynamoDBAdapter.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamoDBAdapter_Ping(t *testing.T) {
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Ping(); (err != nil) != tt.wantErr {
				t.Errorf("DynamoDBAdapter.Ping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamoDBAdapter_GetType(t *testing.T) {
	tests := []struct {
		name string
		s    *DynamoDBAdapter
		want StorageAdapterType
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetType(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DynamoDBAdapter.GetType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_GetProvider(t *testing.T) {
	tests := []struct {
		name string
		s    *DynamoDBAdapter
		want StorageProviders
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetProvider(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DynamoDBAdapter.GetProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_GetSchemaName(t *testing.T) {
	tests := []struct {
		name string
		s    *DynamoDBAdapter
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetSchemaName(); got != tt.want {
				t.Errorf("DynamoDBAdapter.GetSchemaName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_CreateSchema(t *testing.T) {
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.CreateSchema(); (err != nil) != tt.wantErr {
				t.Errorf("DynamoDBAdapter.CreateSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamoDBAdapter_CreateMigrationTable(t *testing.T) {
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.CreateMigrationTable(); (err != nil) != tt.wantErr {
				t.Errorf("DynamoDBAdapter.CreateMigrationTable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamoDBAdapter_UpdateMigrationTable(t *testing.T) {
	type args struct {
		id   int
		name string
		desc string
	}
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.UpdateMigrationTable(tt.args.id, tt.args.name, tt.args.desc); (err != nil) != tt.wantErr {
				t.Errorf("DynamoDBAdapter.UpdateMigrationTable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamoDBAdapter_GetLatestMigration(t *testing.T) {
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		want    int
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.GetLatestMigration()
			if (err != nil) != tt.wantErr {
				t.Fatalf("DynamoDBAdapter.GetLatestMigration() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("DynamoDBAdapter.GetLatestMigration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_Create(t *testing.T) {
	type args struct {
		item   any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Create(tt.args.item, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("DynamoDBAdapter.Create() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamoDBAdapter_Get(t *testing.T) {
	type args struct {
		dest   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Get(tt.args.dest, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("DynamoDBAdapter.Get() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamoDBAdapter_Update(t *testing.T) {
	type args struct {
		item   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Update(tt.args.item, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("DynamoDBAdapter.Update() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamoDBAdapter_Delete(t *testing.T) {
	type args struct {
		item   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Delete(tt.args.item, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("DynamoDBAdapter.Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDynamoDBAdapter_executePaginatedQuery(t *testing.T) {
	type args struct {
		dest    any
		limit   int
		cursor  string
		builder dynamoQueryBuilder
	}
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		args    args
		want    string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.executePaginatedQuery(tt.args.dest, tt.args.limit, tt.args.cursor, tt.args.builder)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DynamoDBAdapter.executePaginatedQuery() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("DynamoDBAdapter.executePaginatedQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_List(t *testing.T) {
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
		s       *DynamoDBAdapter
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
				t.Fatalf("DynamoDBAdapter.List() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("DynamoDBAdapter.List() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_Search(t *testing.T) {
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
		s       *DynamoDBAdapter
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
				t.Fatalf("DynamoDBAdapter.Search() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("DynamoDBAdapter.Search() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_Count(t *testing.T) {
	type args struct {
		dest   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
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
				t.Fatalf("DynamoDBAdapter.Count() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("DynamoDBAdapter.Count() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_Query(t *testing.T) {
	type args struct {
		dest      any
		statement string
		limit     int
		cursor    string
		params    []map[string]any
	}
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
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
				t.Fatalf("DynamoDBAdapter.Query() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("DynamoDBAdapter.Query() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_getTableName(t *testing.T) {
	type args struct {
		obj any
	}
	tests := []struct {
		name string
		s    *DynamoDBAdapter
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.getTableName(tt.args.obj); got != tt.want {
				t.Errorf("DynamoDBAdapter.getTableName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_buildFilter(t *testing.T) {
	type args struct {
		filter map[string]any
	}
	tests := []struct {
		name string
		s    *DynamoDBAdapter
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.buildFilter(tt.args.filter); got != tt.want {
				t.Errorf("DynamoDBAdapter.buildFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDynamoDBAdapter_buildParams(t *testing.T) {
	type args struct {
		filter map[string]any
	}
	tests := []struct {
		name    string
		s       *DynamoDBAdapter
		args    args
		want    []types.AttributeValue
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.buildParams(tt.args.filter)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DynamoDBAdapter.buildParams() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DynamoDBAdapter.buildParams() = %v, want %v", got, tt.want)
			}
		})
	}
}
