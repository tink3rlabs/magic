package observability

import (
	"fmt"

	"github.com/tink3rlabs/magic/telemetry"
)

// projectLabels converts an observation's ordered []Label slice to
// the positional []string required by prometheus.CounterVec et al.,
// in the order declared by def.Labels.
//
// When strict is true (AllowUndeclaredLabels is false on the
// backend) observations that carry unexpected label keys cause
// the function to return an error; the caller drops the
// observation and emits a WarnOnce.
//
// Missing declared labels are filled with the empty string rather
// than errored on, matching Prometheus convention where an empty
// label value is still a valid series.
func projectLabels(def telemetry.MetricDefinition, strict bool, labels []telemetry.Label) ([]string, error) {
	if len(def.Labels) == 0 {
		if strict && len(labels) > 0 {
			return nil, fmt.Errorf("metric %q has no declared labels but got %d", def.Name, len(labels))
		}
		return nil, nil
	}

	values := make([]string, len(def.Labels))
	seen := make(map[string]bool, len(labels))

	for _, l := range labels {
		idx := -1
		for i, k := range def.Labels {
			if k == l.Key {
				idx = i
				break
			}
		}
		if idx < 0 {
			if strict {
				return nil, fmt.Errorf("metric %q: undeclared label %q", def.Name, l.Key)
			}
			continue
		}
		values[idx] = l.Value
		seen[l.Key] = true
	}

	return values, nil
}

// labelSuppressionKey returns a stable WarnOnce key for a given
// metric + error message so callers cannot spam the log when the
// same bad call site fires repeatedly.
func labelSuppressionKey(metricName, reason string) string {
	return "observability.labels:" + metricName + ":" + reason
}
