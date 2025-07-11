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

func (s *SNSPublisher) Publish(topic string, message string, params map[string]any) error {
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
	if (filterKey != nil && filterKey != "") && (filterValue != nil && filterValue != "") {
		publishInput.MessageAttributes = map[string]types.MessageAttributeValue{
			filterKey.(string): {DataType: aws.String("String"), StringValue: aws.String(filterValue.(string))},
		}
	}

	ctx := context.TODO()
	_, err := s.Client.Publish(ctx, &publishInput)
	return err
}
