package pubsub

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/tink3rlabs/magic/logger"
)

type SNSPublisher struct {
	Client *sns.Client
	config map[string]string
}

func GetSNSPublisher(config map[string]string) *SNSPublisher {
	s := SNSPublisher{config: config}
	cfg, err := awsconfig.LoadDefaultConfig(context.TODO())

	if s.config["region"] != "" {
		slog.Debug(fmt.Sprintf("using region override: %s", s.config["region"]))
		cfg.Region = s.config["region"]
	}
	if (s.config["access_key"] != "") && s.config["secret_key"] != "" {
		slog.Debug("using credentials from config file")
		cfg.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			s.config["access_key"],
			s.config["secret_key"],
			"",
		))
	}

	if err != nil {
		logger.Fatal("failed to create SNS publisher", slog.Any("error", err.Error()))
	}

	s.Client = sns.NewFromConfig(cfg, func(o *sns.Options) {
		if config["endpoint"] != "" {
			slog.Debug(fmt.Sprintf("using endpoint override: %s", config["endpoint"]))
			o.BaseEndpoint = aws.String(config["endpoint"])
		}
	})

	return &s
}

// Publish delegates to PublishContext with context.Background so
// non-context-aware callers still get the full publish path.
func (s *SNSPublisher) Publish(topic string, message string, params map[string]any) error {
	return s.PublishContext(context.Background(), topic, message, params)
}

// PublishContext sends a single message to an SNS topic, honoring
// the caller's context for deadline/cancellation propagation and
// merging any wrapper-injected trace attributes from
// params[MessageAttributesParamKey] into the SDK's
// MessageAttributes. Existing filterKey/filterValue support is
// preserved so calling conventions for older code paths continue
// to work.
func (s *SNSPublisher) PublishContext(ctx context.Context, topic string, message string, params map[string]any) error {
	publishInput := sns.PublishInput{TopicArn: aws.String(topic), Message: aws.String(message)}

	groupId := params["groupId"]
	dedupId := params["dedupId"]
	filterKey := params["filterKey"]
	filterValue := params["filterValue"]

	if groupId != nil && groupId != "" {
		publishInput.MessageGroupId = aws.String(groupId.(string))
	}
	if dedupId != nil && dedupId != "" {
		publishInput.MessageDeduplicationId = aws.String(dedupId.(string))
	}

	// MessageAttributes are populated from two sources:
	//   1. wrapper-injected propagator headers carried in
	//      params[MessageAttributesParamKey] as map[string]string
	//   2. legacy filterKey/filterValue params
	// The filter entry wins on key collision so existing
	// call sites keep the same observable behavior.
	var attrs map[string]types.MessageAttributeValue
	if injected, ok := params[MessageAttributesParamKey].(map[string]string); ok && len(injected) > 0 {
		attrs = make(map[string]types.MessageAttributeValue, len(injected)+1)
		for k, v := range injected {
			attrs[k] = types.MessageAttributeValue{
				DataType:    aws.String("String"),
				StringValue: aws.String(v),
			}
		}
	}
	if (filterKey != nil && filterKey != "") && (filterValue != nil && filterValue != "") {
		if attrs == nil {
			attrs = map[string]types.MessageAttributeValue{}
		}
		attrs[filterKey.(string)] = types.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(filterValue.(string)),
		}
	}
	if len(attrs) > 0 {
		publishInput.MessageAttributes = attrs
	}

	_, err := s.Client.Publish(ctx, &publishInput)
	return err
}
