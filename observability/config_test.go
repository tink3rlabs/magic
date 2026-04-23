package observability

import (
	"strings"
	"testing"
)

func TestConfigValidateRequiresServiceName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MetricsMode = MetricsModePrometheus
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "ServiceName") {
		t.Errorf("expected error about ServiceName, got %v", err)
	}
}

func TestConfigValidateRequiresMetricsMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ServiceName = "svc"
	if err := cfg.validate(); err == nil || !strings.Contains(err.Error(), "MetricsMode") {
		t.Errorf("expected error about MetricsMode, got %v", err)
	}
}

func TestConfigValidateSamplingRatioRange(t *testing.T) {
	ok := 0.5
	neg := -0.1
	over := 1.5

	cfg := DefaultConfig()
	cfg.ServiceName = "svc"
	cfg.MetricsMode = MetricsModePrometheus

	cfg.SamplingRatio = &ok
	if err := cfg.validate(); err != nil {
		t.Errorf("0.5 should be valid, got %v", err)
	}

	cfg.SamplingRatio = &neg
	if err := cfg.validate(); err == nil {
		t.Error("negative sampling ratio should be rejected")
	}

	cfg.SamplingRatio = &over
	if err := cfg.validate(); err == nil {
		t.Error("sampling ratio > 1 should be rejected")
	}
}

func TestConfigValidateNamespace(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ServiceName = "svc"
	cfg.MetricsMode = MetricsModePrometheus

	cfg.MetricsNamespace = "good_prefix"
	if err := cfg.validate(); err != nil {
		t.Errorf("good_prefix should validate, got %v", err)
	}

	cfg.MetricsNamespace = "bad-prefix"
	if err := cfg.validate(); err == nil {
		t.Error("'bad-prefix' should fail (dash not allowed)")
	}
}

func TestMetricsModeValid(t *testing.T) {
	if !MetricsModePrometheus.Valid() || !MetricsModeOTLP.Valid() {
		t.Error("built-in modes must be valid")
	}
	if MetricsMode("invalid").Valid() {
		t.Error("arbitrary strings must not be valid")
	}
}
