package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func countersFromScrapeRaw(t *testing.T, obs *Observer, metric string, labelMatchers ...string) float64 {
	t.Helper()
	scrape := httptest.NewServer(obs.MetricsHandler())
	t.Cleanup(scrape.Close)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, scrape.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return sumMatchingLines(t, resp, metric, labelMatchers)
}
