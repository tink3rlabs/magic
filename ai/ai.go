package ai

import (
	"errors"
	"sync"
)

type AIAdapter interface {
	GenerateContent(request AIRequest) (AIResponse, error)
	GetType() AIAdapterType
	GetProvider() AIProvider
	// TODO: Tool calling capabilities
}

type AIAdapterType string

type AIProvider string

const (
	OPENAI_ADAPTER    AIAdapterType = "openai"
	GEMINI_ADAPTER    AIAdapterType = "gemini"
	ANTHROPIC_ADAPTER AIAdapterType = "anthropic"
)

const (
	OPENAI_PROVIDER    AIProvider = "openai"
	GOOGLE_PROVIDER    AIProvider = "google"
	ANTHROPIC_PROVIDER AIProvider = "anthropic"
)

type AIRequest struct {
	Model    string
	Messages []AIMessage
	// TODO: Params
}

type AIMessage struct {
	Role    string // e.g., "user", "assistant", "system"
	Content string
}

type AIResponse struct {
	Content string
}

type AIAdapterFactory struct{}

func (f AIAdapterFactory) GetInstance(adapterType AIAdapterType, config map[string]string) (AIAdapter, error) {
	switch adapterType {
	case OPENAI_ADAPTER:
		return GetOpenAIAdapterInstance(config)
	default:
		return nil, errors.New("this AI adapter type isn't supported")
	}
}

var (
	openAIAdapterInstance *OpenAIAdapter
	openAIAdapterLock     = &sync.Mutex{}
)
