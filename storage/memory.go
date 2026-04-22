package storage

import (
	"context"
	"sync"
)

var memoryAdapterLock = &sync.Mutex{}

type MemoryAdapter struct {
	DB *SQLAdapter
}

var memoryAdapterInstance *MemoryAdapter

func GetMemoryAdapterInstance() *MemoryAdapter {
	if memoryAdapterInstance == nil {
		memoryAdapterLock.Lock()
		defer memoryAdapterLock.Unlock()
		if memoryAdapterInstance == nil {
			// Memory adapter simply uses the SQLAdapter without persistance to disk
			// The SQLITE database will just be stored in memory and not written to a file
			config := map[string]string{
				"provider": "sqlite",
			}
			db := GetSQLAdapterInstance(config)
			memoryAdapterInstance = &MemoryAdapter{DB: db}
		}
	}
	return memoryAdapterInstance
}

func (m *MemoryAdapter) Execute(s string) error {
	return m.ExecuteContext(context.Background(), s)
}

func (m *MemoryAdapter) ExecuteContext(ctx context.Context, s string) error {
	return m.DB.ExecuteContext(ctx, s)
}

func (m *MemoryAdapter) Ping() error {
	return m.PingContext(context.Background())
}

func (m *MemoryAdapter) PingContext(ctx context.Context) error {
	return m.DB.PingContext(ctx)
}

func (m *MemoryAdapter) GetType() StorageAdapterType {
	return MEMORY
}

func (m *MemoryAdapter) GetProvider() StorageProviders {
	return SQLITE
}

func (m *MemoryAdapter) GetSchemaName() string {
	return ""
}

func (m *MemoryAdapter) CreateSchema() error {
	return m.DB.CreateSchema()
}

func (m *MemoryAdapter) CreateMigrationTable() error {
	return m.DB.CreateMigrationTable()
}

func (m *MemoryAdapter) UpdateMigrationTable(id int, name string, desc string) error {
	return m.DB.UpdateMigrationTable(id, name, desc)
}

func (m *MemoryAdapter) GetLatestMigration() (int, error) {
	statement := "SELECT max(id) from migrations"
	var latestMigration int

	result := m.DB.DB.Raw(statement).Scan(&latestMigration)
	if result.Error != nil {
		//either a real issue or there are no migrations yet check if we can query the migration table
		var count int
		statement = "SELECT count(*) from migrations"
		countResult := m.DB.DB.Raw(statement).Scan(&count)
		if countResult.Error != nil {
			return latestMigration, result.Error
		}
	}
	return latestMigration, nil
}

func (m *MemoryAdapter) Create(item any, params ...map[string]any) error {
	return m.CreateContext(context.Background(), item, params...)
}

func (m *MemoryAdapter) CreateContext(ctx context.Context, item any, params ...map[string]any) error {
	return m.DB.CreateContext(ctx, item)
}

func (m *MemoryAdapter) Get(dest any, filter map[string]any, params ...map[string]any) error {
	return m.GetContext(context.Background(), dest, filter, params...)
}

func (m *MemoryAdapter) GetContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) error {
	return m.DB.GetContext(ctx, dest, filter)
}

func (m *MemoryAdapter) Update(item any, filter map[string]any, params ...map[string]any) error {
	return m.UpdateContext(context.Background(), item, filter, params...)
}

func (m *MemoryAdapter) UpdateContext(ctx context.Context, item any, filter map[string]any, params ...map[string]any) error {
	return m.DB.UpdateContext(ctx, item, filter)
}

func (m *MemoryAdapter) Delete(item any, filter map[string]any, params ...map[string]any) error {
	return m.DeleteContext(context.Background(), item, filter, params...)
}

func (m *MemoryAdapter) DeleteContext(ctx context.Context, item any, filter map[string]any, params ...map[string]any) error {
	return m.DB.DeleteContext(ctx, item, filter)
}

func (m *MemoryAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	return m.ListContext(context.Background(), dest, sortKey, filter, limit, cursor, params...)
}

func (m *MemoryAdapter) ListContext(ctx context.Context, dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	return m.DB.ListContext(ctx, dest, sortKey, filter, limit, cursor)
}

func (m *MemoryAdapter) Search(dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
	return m.SearchContext(context.Background(), dest, sortKey, query, limit, cursor, params...)
}

func (m *MemoryAdapter) SearchContext(ctx context.Context, dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
	return m.DB.SearchContext(ctx, dest, sortKey, query, limit, cursor)
}

func (m *MemoryAdapter) Count(dest any, filter map[string]any, params ...map[string]any) (int64, error) {
	return m.CountContext(context.Background(), dest, filter, params...)
}

func (m *MemoryAdapter) CountContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) (int64, error) {
	return m.DB.CountContext(ctx, dest, filter)
}

func (m *MemoryAdapter) Query(dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
	return m.QueryContext(context.Background(), dest, statement, limit, cursor, params...)
}

func (m *MemoryAdapter) QueryContext(ctx context.Context, dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
	return m.DB.QueryContext(ctx, dest, statement, limit, cursor)
}
