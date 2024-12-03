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

func (s PublisherFactory) GetInstance(publisherType PublisherType, config any) (Publisher, error) {
	if config == nil {
		config = make(map[string]string)
	}
	switch publisherType {
	case SNS:
		return GetSNSPublisher(config.(map[string]string)), nil
	default:
		return nil, errors.New("this publisher type isn't supported")
	}
}
