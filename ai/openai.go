package ai

import (
	"context"
	"errors"
	"fmt"
	"github.com/sashabaranov/go-openai"
)

type OpenAIAdapter struct {
	client *openai.Client
	config map[string]string
}

func GetOpenAIAdapterInstance(config map[string]string) (AIAdapter, error) {
	if openAIAdapterInstance == nil {
		openAIAdapterLock.Lock()
		defer openAIAdapterLock.Unlock()
		if openAIAdapterInstance == nil {
			apiKey := config["api_key"]
			baseURL := config["base_url"]
			// TODO: Add more options like thinking etc, params to load from config

			if apiKey == "" {
				return nil, errors.New("OpenAI API key is required for OpenAIAdapter")
			}

			var client *openai.Client
			if baseURL != "" {
				cfg := openai.DefaultConfig(apiKey)
				cfg.BaseURL = baseURL
				client = openai.NewClientWithConfig(cfg)
			} else {
				client = openai.NewClient(apiKey)
			}

			openAIAdapterInstance = &OpenAIAdapter{
				client: client,
				config: config,
			}
		}
	}
	return openAIAdapterInstance, nil
}

func (a *OpenAIAdapter) GenerateContent(request AIRequest) (AIResponse, error) {
	messages := make([]openai.ChatCompletionMessage, len(request.Messages))
	for i, msg := range request.Messages {
		messages[i] = openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	resp, err := a.client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:    request.Model,
			Messages: messages,
		},
	)

	if err != nil {
		return AIResponse{}, fmt.Errorf("failed to generate content from OpenAI: %w", err)
	}

	if len(resp.Choices) == 0 {
		return AIResponse{}, errors.New("no content choices returned from OpenAI")
	}

	return AIResponse{
		Content: resp.Choices[0].Message.Content,
	}, nil
}

func (a *OpenAIAdapter) GetType() AIAdapterType {
	return OPENAI_ADAPTER
}

func (a *OpenAIAdapter) GetProvider() AIProvider {
	return OPENAI_PROVIDER
}
