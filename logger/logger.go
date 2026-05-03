package logger

import (
	"log/slog"
	"os"
)

type Config struct {
	Level slog.Level
	JSON  bool
}

// mapLogLevel maps a string log level from config to slog.Level
func MapLogLevel(levelStr string) slog.Level {
	switch levelStr {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo // default to Info level
	}
}

func Init(config *Config) {

	var handler slog.Handler

	// Choose the handler based on the format and log level from the config
	if config.JSON {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: config.Level})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: config.Level})
	}

	// Wrap the handler so that any *Context slog call made while a
	// valid OpenTelemetry SpanContext is on the context carries
	// trace_id and span_id attributes automatically. The wrapper
	// is cheap (a single SpanContextFromContext call per record)
	// and is a no-op when tracing has not been initialized.
	handler = newTraceHandler(handler)

	// Initialize the logger with the selected handler
	logger := slog.New(handler)

	//Set the global default logger this is the logger that will be used when slog.<LevelName>() functions are used
	slog.SetDefault(logger)

}

// slog doesn't have fatal, hence creating the function
func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
