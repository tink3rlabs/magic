package pubsub

import (
	"errors"
)

type Publisher interface {
	Publish(topic string, message string, params map[string]any) error
}

const (
	SNS PublisherType = "sns"
)

type PublisherType string
type PublisherFactory struct{}

// GetInstance constructs the requested publisher and wraps it
// with the telemetry-aware instrumentation adapter. When the
// observability stack has not been initialized the wrapper uses
// no-op instruments and adds negligible overhead; when it has,
// the wrapper emits a `pubsub.publish` producer span, injects W3C
// trace context into outbound message metadata, and records the
// built-in pubsub metrics.
func (s PublisherFactory) GetInstance(publisherType PublisherType, config any) (Publisher, error) {
	if config == nil {
		config = make(map[string]string)
	}
	var inner Publisher
	switch publisherType {
	case SNS:
		inner = GetSNSPublisher(config.(map[string]string))
	default:
		return nil, errors.New("this publisher type isn't supported")
	}
	return wrapForTelemetry(inner, string(publisherType)), nil
}
