package deepseek

import (
	"errors"

	"github.com/sashabaranov/go-openai"

	chatgpt "github.com/18721889353/sunshine/pkg/aicli/chatgpt"
)

// https://api-docs.deepseek.com/

const (
	BaseURL = "https://api.deepseek.com/"

	ModelDeepSeekChat     = "deepseek-chat"
	ModelDeepSeekReasoner = "deepseek-reasoner"
	DefaultModel          = ModelDeepSeekChat

	TemperatureCodeGeneration float32 = 0.0 // code generation, mathematical problem-solving
	TemperatureDataAnalysis   float32 = 1.0 // data extraction, analysis
	TemperatureDataChat       float32 = 1.3 // universal chat, translation
	TemperatureDataCreative   float32 = 2.0 // creative writing, poetry writing

	RoleTypeGopher  = chatgpt.RoleTypeGopher
	RoleTypeGeneral = chatgpt.RoleTypeGeneral
)

type (
	Client       = chatgpt.Client
	ClientOption = chatgpt.ClientOption
)

var (
	WithMaxTokens   = chatgpt.WithMaxTokens
	WithModel       = chatgpt.WithModel
	WithTemperature = chatgpt.WithTemperature
	WithRole        = chatgpt.WithRole
	WithUseContext  = chatgpt.WithUseContext
)

// NewClient creates a new chat client.
func NewClient(apiKey string, opts ...ClientOption) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("API key cannot be empty")
	}

	c, err := chatgpt.NewClient(apiKey, opts...)
	if err != nil {
		return nil, err
	}

	if c.ModelName == chatgpt.DefaultModel {
		c.ModelName = DefaultModel
	}

	config := openai.DefaultConfig(apiKey)
	config.BaseURL = BaseURL
	c.Cli = openai.NewClientWithConfig(config)

	return c, nil
}
