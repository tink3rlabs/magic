package main

import (
	"context"
	"embed"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	magicerrors "github.com/tink3rlabs/magic/errors"
	"github.com/tink3rlabs/magic/health"
	magiclogger "github.com/tink3rlabs/magic/logger"
	"github.com/tink3rlabs/magic/middlewares"
	"github.com/tink3rlabs/magic/observability"
	"github.com/tink3rlabs/magic/storage"
	"github.com/tink3rlabs/magic/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type Item struct {
	Tenant      string `json:"tenant"`
	Id          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

//go:embed config
var configFS embed.FS

func initLogger() {
	level := magiclogger.MapLogLevel(strings.ToLower(strings.TrimSpace(firstNonEmpty(os.Getenv("LOGGER_LEVEL"), "info"))))
	json := strings.EqualFold(strings.TrimSpace(firstNonEmpty(os.Getenv("LOGGER_JSON"), "false")), "true")
	magiclogger.Init(&magiclogger.Config{
		Level: level,
		JSON:  json,
	})
}

func main() {
	/*
		This single example shows both storage + observability.

		Run locally (Prometheus mode):
		  METRICS_MODE=prometheus ENABLE_TRACING=false LOGGER_LEVEL=info LOGGER_JSON=false go run ./examples

		  # service endpoints
		  curl -i http://localhost:8080/health/liveness
		  curl -i http://localhost:8080/health/readiness
		  curl -s http://localhost:8080/api-docs

		  # generate traffic + storage writes/reads
		  curl -i -X POST http://localhost:8080/orders
		  curl -i http://localhost:8080/orders/<id>

		  # verify app metrics directly
		  curl -s http://localhost:8080/metrics | grep -E "http_requests_total|magic_storage_operations_total|orders_created_total"

		Run locally (OTLP mode + collector):
		  # start collector with provided config
		  docker run --rm --name otelcol-magic-example --network=host \
		    -v "/workspaces/magic/examples/otel-collector.yml:/etc/otelcol-contrib/config.yaml:ro" \
		    otel/opentelemetry-collector-contrib:latest

		  METRICS_MODE=otlp ENABLE_TRACING=true OTLP_ENDPOINT=localhost:4317 LOGGER_LEVEL=debug LOGGER_JSON=true go run ./examples

		  # app /metrics is 404 in OTLP mode by design; verify at collector
		  curl -s http://localhost:9464/metrics | grep -E "http_requests_total|magic_storage_operations_total|orders_created_total"

		  # verify trace/log correlation fields
		  # (with LOGGER_JSON=true you'll see trace_id + span_id on *Context slog calls)
		  # docker logs otelcol-magic-example
	*/
	storage.ConfigFs = configFS
	initLogger()

	mode := metricsModeFromEnv(os.Getenv("METRICS_MODE"))
	otlpEndpoint := firstNonEmpty(os.Getenv("OTLP_ENDPOINT"), "localhost:4317")
	enableTracing := strings.EqualFold(os.Getenv("ENABLE_TRACING"), "true")

	cfg := observability.DefaultConfig()
	cfg.ServiceName = "magic-storage-observability-example"
	cfg.MetricsMode = mode
	cfg.EnableTracing = enableTracing
	cfg.TracesOTLPEndpoint = otlpEndpoint
	cfg.TracesOTLPInsecure = true
	cfg.MetricsOTLPEndpoint = otlpEndpoint
	cfg.MetricsOTLPInsecure = true

	obs, err := observability.Init(context.Background(), cfg)
	if err != nil {
		magiclogger.Fatal("failed to initialize observability", slog.Any("error", err))
	}
	defer func() { _ = obs.Shutdown(context.Background()) }()

	// Swap provider/config here to try PostgreSQL/DynamoDB/Cosmos.
	adapterCfg := map[string]string{}
	s, err := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, adapterCfg)
	if err != nil {
		magiclogger.Fatal("failed to create storage adapter", slog.Any("error", err))
	}
	if err := ping(context.Background(), s); err != nil {
		magiclogger.Fatal("storage ping failed", slog.Any("error", err))
	}
	storage.NewDatabaseMigration(s).Migrate()

	ordersCreated, err := obs.Counter(telemetry.MetricDefinition{
		Name:   "orders_created_total",
		Help:   "Total orders created by channel and result.",
		Kind:   telemetry.KindCounter,
		Labels: []string{"channel", "result"},
	})
	if err != nil {
		magiclogger.Fatal("failed to register custom metric", slog.Any("error", err))
	}

	r := chi.NewRouter()
	r.Use(
		render.SetContentType(render.ContentTypeJSON),
		middleware.RedirectSlashes,
		middleware.Recoverer,
		middlewares.Observability(obs),
		magiclogger.ChiRequestLogger(magiclogger.RequestLoggerOptions{
			SkipPaths:        []string{"/metrics"},
			SkipPathPrefixes: []string{"/health/"},
		}),
	)

	openAPISpec, err := buildOpenAPISpec()
	if err != nil {
		magiclogger.Fatal("failed to build openapi spec", slog.Any("error", err))
	}

	r.Get("/api-docs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(openAPISpec)
	})

	r.Get("/health/liveness", func(w http.ResponseWriter, r *http.Request) {
		render.Status(r, http.StatusNoContent)
		render.NoContent(w, r)
	})

	healthChecker := health.NewHealthChecker(s)
	errHandler := middlewares.ErrorHandler{}
	r.Get("/health/readiness", errHandler.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		if err := healthChecker.Check(true, nil); err != nil {
			return &magicerrors.ServiceUnavailable{Message: err.Error()}
		}
		render.Status(r, http.StatusNoContent)
		render.NoContent(w, r)
		return nil
	}))

	r.Handle("/metrics", obs.MetricsHandler())
	ordersTracer := obs.TracerProvider().Tracer("github.com/tink3rlabs/magic/examples/orders")

	r.Post("/orders", func(w http.ResponseWriter, req *http.Request) {
		ctx, span := ordersTracer.Start(req.Context(), "orders.create")
		defer span.End()
		span.SetAttributes(
			attribute.String("orders.operation", "create"),
			attribute.String("orders.tenant", "example.io"),
		)
		req = req.WithContext(ctx)
		slog.InfoContext(req.Context(), "creating order")
		id, err := uuid.NewV7()
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "id_generation_failed")
			slog.ErrorContext(req.Context(), "failed generating order id", slog.Any("error", err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		item := Item{
			Tenant:      "example.io",
			Id:          id.String(),
			Kind:        "order",
			Name:        "sample-order",
			Description: "created via example endpoint",
		}

		if err := createItem(req.Context(), s, item); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "storage_create_failed")
			slog.ErrorContext(req.Context(), "failed creating storage item", slog.Any("error", err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		span.SetAttributes(attribute.String("orders.id", item.Id))
		span.AddEvent("orders.created")
		ordersCreated.Add(1, telemetry.Labels("channel", "api", "result", "ok")...)
		_ = json.NewEncoder(w).Encode(item)
	})

	r.Get("/orders/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		ctx, span := ordersTracer.Start(req.Context(), "orders.get")
		defer span.End()
		span.SetAttributes(
			attribute.String("orders.operation", "get"),
			attribute.String("orders.id", id),
		)
		req = req.WithContext(ctx)
		slog.InfoContext(req.Context(), "getting order", slog.String("id", id))
		var item Item
		if err := getItem(req.Context(), s, &item, map[string]any{"tenant": "example.io", "id": id}); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "order_not_found")
			slog.WarnContext(req.Context(), "order not found", slog.String("id", id), slog.Any("error", err))
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		span.AddEvent("orders.fetched")
		_ = json.NewEncoder(w).Encode(item)
	})

	slog.Info("example running",
		slog.String("addr", ":8080"),
		slog.String("metrics_mode", string(mode)),
		slog.Bool("tracing", enableTracing),
	)
	if err := http.ListenAndServe(":8080", r); err != nil {
		magiclogger.Fatal("server stopped unexpectedly", slog.Any("error", err))
	}
}

func createItem(ctx context.Context, s storage.StorageAdapter, item Item) error {
	if cs, ok := s.(storage.ContextualStorageAdapter); ok {
		return cs.CreateContext(ctx, item)
	}
	return s.Create(item)
}

func getItem(ctx context.Context, s storage.StorageAdapter, dest any, filter map[string]any) error {
	if cs, ok := s.(storage.ContextualStorageAdapter); ok {
		return cs.GetContext(ctx, dest, filter)
	}
	return s.Get(dest, filter)
}

func ping(ctx context.Context, s storage.StorageAdapter) error {
	if cs, ok := s.(storage.ContextualStorageAdapter); ok {
		return cs.PingContext(ctx)
	}
	return s.Ping()
}

func metricsModeFromEnv(v string) observability.MetricsMode {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "prometheus", "prom":
		return observability.MetricsModePrometheus
	case "otlp":
		return observability.MetricsModeOTLP
	default:
		slog.Warn("unknown METRICS_MODE, defaulting to prometheus", slog.String("value", v))
		return observability.MetricsModePrometheus
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func buildOpenAPISpec() ([]byte, error) {
	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "Magic Example API",
			"version": "1.0.0",
		},
		"paths": map[string]any{
			"/orders": map[string]any{
				"post": map[string]any{
					"summary": "Create an order item in storage",
					"responses": map[string]any{
						"201": map[string]any{"description": "Created"},
					},
				},
			},
			"/orders/{id}": map[string]any{
				"get": map[string]any{
					"summary": "Get an order item from storage",
					"responses": map[string]any{
						"200": map[string]any{"description": "OK"},
						"404": map[string]any{"description": "Not Found"},
					},
				},
			},
			"/health/liveness": map[string]any{
				"get": map[string]any{
					"summary": "Liveness check",
					"responses": map[string]any{
						"204": map[string]any{"description": "No Content"},
					},
				},
			},
			"/health/readiness": map[string]any{
				"get": map[string]any{
					"summary": "Readiness check",
					"responses": map[string]any{
						"204": map[string]any{"description": "No Content"},
						"503": map[string]any{"description": "Service Unavailable"},
					},
				},
			},
		},
	}
	return json.Marshal(spec)
}
