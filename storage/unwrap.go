package storage

// TelemetryUnwrapper is implemented by the instrumented adapter returned from
// StorageAdapterFactory.GetInstance after observability instrumentation wraps
// storage. Call UnwrapStorageAdapter to peel one telemetry layer (similar to
// errors.Unwrap), or use UnwrapAdapter for all nested wrappers in one step.
//
// Typical use when you must reach the concrete type—for example to register a
// GORM plugin with sql.DB.Use—otherwise type assertions such as
// s.(*SQLAdapter) fail because s is the wrapper, not *SQLAdapter:
//
//	inner := storage.UnwrapAdapter(s)
//	sqlAdapter := inner.(*storage.SQLAdapter)
//	sqlAdapter.DB.Use(plugin)
//
// Or peel a single layer with:
//
//	if u, ok := s.(storage.TelemetryUnwrapper); ok {
//		_ = u.UnwrapStorageAdapter()
//	}
type TelemetryUnwrapper interface {
	UnwrapStorageAdapter() StorageAdapter
}

// UnwrapAdapter returns the inner StorageAdapter when s is (directly or
// transitively) the telemetry instrumented wrapper used by magic; otherwise it
// returns s unchanged.
//
// Call this before type assertions to concrete adapter implementations (for
// example *SQLAdapter, *MemoryAdapter, *DynamoDBAdapter), including before
// registering GORM plugins or accessing embedded *gorm.DB, when the adapter may
// have been wrapped after global telemetry was initialized.
func UnwrapAdapter(s StorageAdapter) StorageAdapter {
	var cur = s
	for {
		w, ok := cur.(*instrumentedAdapter)
		if !ok {
			return cur
		}
		cur = w.inner
	}
}
