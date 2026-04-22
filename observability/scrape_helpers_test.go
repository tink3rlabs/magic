package observability

import (
	"bufio"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// sumMatchingLines parses a Prometheus text-format response and
// returns the sum of values for sample lines that:
//  1. Start with the given metric name (plus '{' or ' ').
//  2. Contain every labelMatcher string exactly as given.
//
// The helpers live in their own _test.go file so they can be
// shared across several *_test.go files in this package.
func sumMatchingLines(t *testing.T, resp *http.Response, metric string, labelMatchers []string) float64 {
	t.Helper()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var total float64
	prefixed := metric + "{"
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if !strings.HasPrefix(line, prefixed) && !strings.HasPrefix(line, metric+" ") {
			continue
		}
		matched := true
		for _, m := range labelMatchers {
			if !strings.Contains(line, m) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		idx := strings.LastIndex(line, " ")
		if idx < 0 {
			continue
		}
		v, err := strconv.ParseFloat(line[idx+1:], 64)
		if err != nil {
			continue
		}
		total += v
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan scrape: %v", err)
	}
	return total
}
