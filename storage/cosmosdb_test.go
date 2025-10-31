package storage

import (
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos"
)

func TestGetCosmosDBAdapterInstance(t *testing.T) {
	type args struct {
		config map[string]string
	}
	tests := []struct {
		name string
		args args
		want *CosmosDBAdapter
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetCosmosDBAdapterInstance(tt.args.config); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetCosmosDBAdapterInstance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_OpenConnection(t *testing.T) {
	tests := []struct {
		name string
		s    *CosmosDBAdapter
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.s.OpenConnection()
		})
	}
}

func TestCosmosDBAdapter_Execute(t *testing.T) {
	type args struct {
		statement string
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Execute(tt.args.statement); (err != nil) != tt.wantErr {
				t.Errorf("CosmosDBAdapter.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCosmosDBAdapter_Ping(t *testing.T) {
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Ping(); (err != nil) != tt.wantErr {
				t.Errorf("CosmosDBAdapter.Ping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCosmosDBAdapter_GetType(t *testing.T) {
	tests := []struct {
		name string
		s    *CosmosDBAdapter
		want StorageAdapterType
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetType(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CosmosDBAdapter.GetType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_GetProvider(t *testing.T) {
	tests := []struct {
		name string
		s    *CosmosDBAdapter
		want StorageProviders
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetProvider(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CosmosDBAdapter.GetProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_GetSchemaName(t *testing.T) {
	tests := []struct {
		name string
		s    *CosmosDBAdapter
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.GetSchemaName(); got != tt.want {
				t.Errorf("CosmosDBAdapter.GetSchemaName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_CreateSchema(t *testing.T) {
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.CreateSchema(); (err != nil) != tt.wantErr {
				t.Errorf("CosmosDBAdapter.CreateSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCosmosDBAdapter_CreateMigrationTable(t *testing.T) {
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.CreateMigrationTable(); (err != nil) != tt.wantErr {
				t.Errorf("CosmosDBAdapter.CreateMigrationTable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCosmosDBAdapter_UpdateMigrationTable(t *testing.T) {
	type args struct {
		id   int
		name string
		desc string
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.UpdateMigrationTable(tt.args.id, tt.args.name, tt.args.desc); (err != nil) != tt.wantErr {
				t.Errorf("CosmosDBAdapter.UpdateMigrationTable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCosmosDBAdapter_GetLatestMigration(t *testing.T) {
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		want    int
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.GetLatestMigration()
			if (err != nil) != tt.wantErr {
				t.Fatalf("CosmosDBAdapter.GetLatestMigration() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("CosmosDBAdapter.GetLatestMigration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_Create(t *testing.T) {
	type args struct {
		item   any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Create(tt.args.item, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("CosmosDBAdapter.Create() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCosmosDBAdapter_Get(t *testing.T) {
	type args struct {
		dest   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Get(tt.args.dest, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("CosmosDBAdapter.Get() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCosmosDBAdapter_Update(t *testing.T) {
	type args struct {
		item   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Update(tt.args.item, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("CosmosDBAdapter.Update() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCosmosDBAdapter_Delete(t *testing.T) {
	type args struct {
		item   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Delete(tt.args.item, tt.args.filter, tt.args.params...); (err != nil) != tt.wantErr {
				t.Errorf("CosmosDBAdapter.Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCosmosDBAdapter_List(t *testing.T) {
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
		s       *CosmosDBAdapter
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
				t.Fatalf("CosmosDBAdapter.List() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("CosmosDBAdapter.List() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_Search(t *testing.T) {
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
		s       *CosmosDBAdapter
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
				t.Fatalf("CosmosDBAdapter.Search() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("CosmosDBAdapter.Search() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_Count(t *testing.T) {
	type args struct {
		dest   any
		filter map[string]any
		params []map[string]any
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
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
				t.Fatalf("CosmosDBAdapter.Count() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("CosmosDBAdapter.Count() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_Query(t *testing.T) {
	type args struct {
		dest      any
		statement string
		limit     int
		cursor    string
		params    []map[string]any
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
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
				t.Fatalf("CosmosDBAdapter.Query() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("CosmosDBAdapter.Query() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_executePaginatedQuery(t *testing.T) {
	type args struct {
		dest          any
		sortKey       string
		sortDirection string
		limit         int
		cursor        string
		filter        map[string]any
		params        []map[string]any
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		args    args
		want    string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.executePaginatedQuery(tt.args.dest, tt.args.sortKey, tt.args.sortDirection, tt.args.limit, tt.args.cursor, tt.args.filter, tt.args.params...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CosmosDBAdapter.executePaginatedQuery() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("CosmosDBAdapter.executePaginatedQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_getContainerName(t *testing.T) {
	type args struct {
		obj any
	}
	tests := []struct {
		name string
		s    *CosmosDBAdapter
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.getContainerName(tt.args.obj); got != tt.want {
				t.Errorf("CosmosDBAdapter.getContainerName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_itemToMap(t *testing.T) {
	type args struct {
		item any
	}
	tests := []struct {
		name string
		s    *CosmosDBAdapter
		args args
		want map[string]interface{}
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.itemToMap(tt.args.item); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CosmosDBAdapter.itemToMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_buildPartitionKey(t *testing.T) {
	type args struct {
		paramMap map[string]any
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		args    args
		want    string
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.buildPartitionKey(tt.args.paramMap)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CosmosDBAdapter.buildPartitionKey() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("CosmosDBAdapter.buildPartitionKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_getPartitionKeyFieldName(t *testing.T) {
	type args struct {
		paramMap map[string]any
	}
	tests := []struct {
		name string
		s    *CosmosDBAdapter
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.getPartitionKeyFieldName(tt.args.paramMap); got != tt.want {
				t.Errorf("CosmosDBAdapter.getPartitionKeyFieldName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_executeQuery(t *testing.T) {
	type args struct {
		containerClient *azcosmos.ContainerClient
		query           string
		paramMap        map[string]any
		queryOptions    *azcosmos.QueryOptions
	}
	tests := []struct {
		name    string
		s       *CosmosDBAdapter
		args    args
		want    azcosmos.QueryItemsResponse
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.executeQuery(tt.args.containerClient, tt.args.query, tt.args.paramMap, tt.args.queryOptions)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CosmosDBAdapter.executeQuery() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CosmosDBAdapter.executeQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_extractParams(t *testing.T) {
	type args struct {
		params []map[string]any
	}
	tests := []struct {
		name string
		s    *CosmosDBAdapter
		args args
		want map[string]any
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.extractParams(tt.args.params...); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CosmosDBAdapter.extractParams() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_extractSortDirection(t *testing.T) {
	type args struct {
		paramMap map[string]any
	}
	tests := []struct {
		name string
		s    *CosmosDBAdapter
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.extractSortDirection(tt.args.paramMap); got != tt.want {
				t.Errorf("CosmosDBAdapter.extractSortDirection() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosmosDBAdapter_buildFilter(t *testing.T) {
	type args struct {
		filter     map[string]any
		paramIndex *int
	}
	tests := []struct {
		name  string
		s     *CosmosDBAdapter
		args  args
		want  string
		want1 []azcosmos.QueryParameter
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := tt.s.buildFilter(tt.args.filter, tt.args.paramIndex)
			if got != tt.want {
				t.Errorf("CosmosDBAdapter.buildFilter() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("CosmosDBAdapter.buildFilter() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
