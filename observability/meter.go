package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// setupMeterProvider builds and returns an OTEL MeterProvider
// backed by the OTLP/gRPC metric exporter, configured with the
// built-in histogram bucket views from histogramViews(). It also
// returns a shutdown function that flushes pending metrics.
//
// This function is only used when MetricsMode is MetricsModeOTLP;
// the Prometheus mode uses prometheusBackend directly with no
// MeterProvider.
func setupMeterProvider(ctx context.Context, cfg Config, res *resource.Resource) (*metric.MeterProvider, func(context.Context) error, error) {
	endpoint := resolveOTLPEndpoint(cfg.MetricsOTLPEndpoint, "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")
	if endpoint == "" {
		return nil, nil, fmt.Errorf("observability: MetricsMode=otlp but no OTLP metrics endpoint configured (set MetricsOTLPEndpoint or OTEL_EXPORTER_OTLP_[METRICS_]ENDPOINT)")
	}

	expOpts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(endpoint),
	}
	if cfg.MetricsOTLPInsecure {
		expOpts = append(expOpts, otlpmetricgrpc.WithInsecure())
	}

	exp, err := otlpmetricgrpc.New(ctx, expOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("observability: create OTLP metric exporter: %w", err)
	}

	interval := cfg.MetricsPushInterval
	if interval == 0 {
		interval = defaultMetricsPushInterval
	}

	reader := metric.NewPeriodicReader(exp, metric.WithInterval(interval))

	opts := []metric.Option{
		metric.WithReader(reader),
		metric.WithResource(res),
	}
	for _, v := range histogramViews() {
		opts = append(opts, metric.WithView(v))
	}

	mp := metric.NewMeterProvider(opts...)
	return mp, mp.Shutdown, nil
}

// histogramViews returns the OTEL Views used to pin explicit
// bucket boundaries for built-in histograms. Custom histograms
// registered at runtime rely on the SDK default aggregation; this
// keeps the MeterProvider configuration static and avoids having
// to rebuild it on every metric registration.
func histogramViews() []metric.View {
	return []metric.View{
		metric.NewView(
			metric.Instrument{Name: HTTPRequestDurationSeconds},
			metric.Stream{
				Aggregation: metric.AggregationExplicitBucketHistogram{
					Boundaries: httpDurationBuckets,
				},
			},
		),
		metric.NewView(
			metric.Instrument{Name: StorageOperationDurationSeconds},
			metric.Stream{
				Aggregation: metric.AggregationExplicitBucketHistogram{
					Boundaries: storageDurationBuckets,
				},
			},
		),
	}
}
