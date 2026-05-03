package storage

import "testing"

func TestTelemetryUnwrapperUnwrapsOneLayer(t *testing.T) {
	inner := &legacyAdapter{provider: "legacy-db"}
	wrapped := wrapForTelemetry(inner)
	u, ok := wrapped.(TelemetryUnwrapper)
	if !ok {
		t.Fatalf("wrapped should implement TelemetryUnwrapper")
	}
	if u.UnwrapStorageAdapter() != inner {
		t.Fatalf("UnwrapStorageAdapter = %T; want *legacyAdapter", u.UnwrapStorageAdapter())
	}
}

func TestUnwrapAdapterStripsInstrumentedWrapper(t *testing.T) {
	inner := &legacyAdapter{provider: "legacy-db"}
	wrapped := wrapForTelemetry(inner)
	if UnwrapAdapter(wrapped) != inner {
		t.Fatalf("UnwrapAdapter = %T; want *legacyAdapter", UnwrapAdapter(wrapped))
	}
}

func TestUnwrapAdapterNoop(t *testing.T) {
	inner := &legacyAdapter{provider: "legacy-db"}
	if UnwrapAdapter(inner) != inner {
		t.Fatalf("UnwrapAdapter of non-wrapper should be identity")
	}
}
