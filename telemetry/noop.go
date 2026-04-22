package telemetry

// noopMetricsBackend is the default MetricsBackend installed
// before observability.Init runs. It accepts any registration and
// discards all observations. It never returns an error.
type noopMetricsBackend struct{}

func (noopMetricsBackend) Counter(MetricDefinition) (Counter, error) {
	return noopCounter{}, nil
}

func (noopMetricsBackend) Histogram(MetricDefinition) (Histogram, error) {
	return noopHistogram{}, nil
}

func (noopMetricsBackend) Gauge(MetricDefinition) (Gauge, error) {
	return noopGauge{}, nil
}

func (noopMetricsBackend) UpDownCounter(MetricDefinition) (UpDownCounter, error) {
	return noopUpDownCounter{}, nil
}

type noopCounter struct{}

func (noopCounter) Add(float64, ...Label) {}

type noopHistogram struct{}

func (noopHistogram) Observe(float64, ...Label) {}

type noopGauge struct{}

func (noopGauge) Set(float64, ...Label) {}

type noopUpDownCounter struct{}

func (noopUpDownCounter) Add(float64, ...Label) {}
