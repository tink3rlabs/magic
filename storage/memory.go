package storage

import (
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
	return m.DB.Execute(s)
}

func (m *MemoryAdapter) Ping() error {
	return m.DB.Ping()
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

func (m *MemoryAdapter) Create(item any) error {
	return m.DB.Create(item)
}

func (m *MemoryAdapter) Get(dest any, filter map[string]any) error {
	return m.DB.Get(dest, filter)
}

func (m *MemoryAdapter) Update(item any, filter map[string]any) error {
	return m.DB.Update(item, filter)
}

func (m *MemoryAdapter) Delete(item any, filter map[string]any) error {
	return m.DB.Delete(item, filter)
}

func (m *MemoryAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string) (string, error) {
	return m.DB.List(dest, sortKey, filter, limit, cursor)
}

func (m *MemoryAdapter) Search(dest any, sortKey string, query string, limit int, cursor string) (string, error) {
	return m.DB.Search(dest, sortKey, query, limit, cursor)
}

func (s *MemoryAdapter) Count(dest any) (int64, error) {
	//TODO: Implement
	var total int64
	return total, nil
}

func (m *MemoryAdapter) Query(dest any, statement string, limit int, cursor string) (string, error) {
	return m.DB.Query(dest, statement, limit, cursor)
}
