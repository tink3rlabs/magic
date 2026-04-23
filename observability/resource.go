package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// buildResource assembles the OTEL Resource that is attached to
// every span and metric emitted by this process. Defaults are
// derived from runtime detection (host.name, process.pid,
// telemetry.sdk.*) and overlaid with the caller-supplied service
// metadata. User-specified ResourceAttributes win unless they
// would clash with a reserved service.* / deployment.* key that
// we explicitly manage.
func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	reserved := map[string]bool{
		string(semconv.ServiceNameKey):    true,
		string(semconv.ServiceVersionKey): true,
		"deployment.environment.name":     true,
	}

	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
	}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}
	if cfg.Environment != "" {
		attrs = append(attrs, attribute.String("deployment.environment.name", cfg.Environment))
	}
	for k, v := range cfg.ResourceAttributes {
		if reserved[k] {
			continue
		}
		attrs = append(attrs, attribute.String(k, v))
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("observability: build resource: %w", err)
	}
	return res, nil
}
