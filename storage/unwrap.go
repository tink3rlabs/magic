package storage

// UnwrapAdapter returns the inner StorageAdapter when s is the telemetry
// instrumented wrapper used by magic; otherwise it returns s unchanged.
//
// Call this before type assertions to concrete adapter implementations (for
// example *DynamoDBAdapter) when the adapter may have been wrapped after
// global telemetry was initialized.
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
