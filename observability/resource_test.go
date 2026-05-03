package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func attrMap(t *testing.T, cfg Config) map[string]string {
	t.Helper()
	res, err := buildResource(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}
	out := map[string]string{}
	for _, kv := range res.Attributes() {
		out[string(kv.Key)] = kv.Value.Emit()
	}
	return out
}

func TestBuildResourceAppliesServiceMetadata(t *testing.T) {
	cfg := Config{
		ServiceName:    "svc",
		ServiceVersion: "1.2.3",
		Environment:    "prod",
	}
	attrs := attrMap(t, cfg)
	if attrs["service.name"] != "svc" {
		t.Errorf("service.name = %q", attrs["service.name"])
	}
	if attrs["service.version"] != "1.2.3" {
		t.Errorf("service.version = %q", attrs["service.version"])
	}
	if attrs["deployment.environment.name"] != "prod" {
		t.Errorf("deployment.environment.name = %q", attrs["deployment.environment.name"])
	}
}

func TestBuildResourceOmitsUnsetOptionalFields(t *testing.T) {
	cfg := Config{ServiceName: "svc"}
	attrs := attrMap(t, cfg)
	if _, ok := attrs["service.version"]; ok {
		t.Error("service.version should not be set when ServiceVersion is empty")
	}
	if _, ok := attrs["deployment.environment.name"]; ok {
		t.Error("deployment.environment.name should not be set when Environment is empty")
	}
}

func TestBuildResourceMergesExtraAttributes(t *testing.T) {
	cfg := Config{
		ServiceName: "svc",
		ResourceAttributes: map[string]string{
			"team":   "infra",
			"region": "us-east-1",
		},
	}
	attrs := attrMap(t, cfg)
	if attrs["team"] != "infra" {
		t.Errorf("team = %q", attrs["team"])
	}
	if attrs["region"] != "us-east-1" {
		t.Errorf("region = %q", attrs["region"])
	}
}

func TestBuildResourceReservedKeysCannotBeOverridden(t *testing.T) {
	cfg := Config{
		ServiceName:    "svc",
		ServiceVersion: "v1",
		Environment:    "prod",
		ResourceAttributes: map[string]string{
			"service.name":                "hijacked",
			"service.version":             "hijacked",
			"deployment.environment.name": "hijacked",
		},
	}
	attrs := attrMap(t, cfg)
	if attrs["service.name"] != "svc" {
		t.Errorf("reserved service.name must not be overwritten: got %q", attrs["service.name"])
	}
	if attrs["service.version"] != "v1" {
		t.Errorf("reserved service.version must not be overwritten: got %q", attrs["service.version"])
	}
	if attrs["deployment.environment.name"] != "prod" {
		t.Errorf("reserved deployment.environment.name must not be overwritten: got %q", attrs["deployment.environment.name"])
	}
}

func TestBuildResourceIncludesTelemetrySDK(t *testing.T) {
	cfg := Config{ServiceName: "svc"}
	res, err := buildResource(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}
	found := false
	for _, kv := range res.Attributes() {
		if kv.Key == attribute.Key("telemetry.sdk.name") {
			found = true
			break
		}
	}
	if !found {
		t.Error("telemetry.sdk.name should be populated by resource.WithTelemetrySDK")
	}
}
