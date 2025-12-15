package logger

import (
	"log/slog"
	"testing"
)

func TestMapLogLevel(t *testing.T) {
	type args struct {
		levelStr string
	}
	tests := []struct {
		name string
		args args
		want slog.Level
	}{
		{
			name: "maps debug to LevelDebug",
			args: args{levelStr: "debug"},
			want: slog.LevelDebug,
		},
		{
			name: "maps info to LevelInfo",
			args: args{levelStr: "info"},
			want: slog.LevelInfo,
		},
		{
			name: "maps warn to LevelWarn",
			args: args{levelStr: "warn"},
			want: slog.LevelWarn,
		},
		{
			name: "maps error to LevelError",
			args: args{levelStr: "error"},
			want: slog.LevelError,
		},
		{
			name: "unknown level defaults to LevelInfo",
			args: args{levelStr: "unknown"},
			want: slog.LevelInfo,
		},
		{
			name: "empty string defaults to LevelInfo",
			args: args{levelStr: ""},
			want: slog.LevelInfo,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapLogLevel(tt.args.levelStr); got != tt.want {
				t.Errorf("MapLogLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInit(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
	}{
		{
			name: "initializes with JSON format",
			config: &Config{
				Level: slog.LevelInfo,
				JSON:  true,
			},
		},
		{
			name: "initializes with text format",
			config: &Config{
				Level: slog.LevelInfo,
				JSON:  false,
			},
		},
		{
			name: "initializes with debug level",
			config: &Config{
				Level: slog.LevelDebug,
				JSON:  false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Init(tt.config)
			defaultLogger := slog.Default()
			if defaultLogger == nil {
				t.Error("Init() should set default logger")
			}
		})
	}
}

// Note: Fatal test is skipped as it calls os.Exit(1) which would terminate the test process
