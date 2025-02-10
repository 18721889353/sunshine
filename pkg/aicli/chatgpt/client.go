package gptclient

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/sashabaranov/go-openai"
)

// https://platform.openai.com/docs/api-reference

const (
	ModelGPT3Dot5Turbo = openai.GPT3Dot5Turbo
	ModelGPT4          = openai.GPT4
	ModelGPT4Turbo     = openai.GPT4Turbo
	ModelGPT4o         = openai.GPT4o // default
	ModelGPT4oMini     = openai.GPT4oMini
	ModelO1Mini        = openai.O1Mini
	ModelO1Preview     = openai.O1Preview

	DefaultModel     = ModelO1Mini
	defaultMaxTokens = 4096

	RoleTypeGopher = "You are a Go Language Coder Assistant, an AI specialized in writing, debugging, " +
		"and explaining Go code. You only provide solutions and explanations for Go programming " +
		"language. Always provide clear and concise code examples, and explain your solutions step by step."
	RoleTypeGeneral = "You are a General Assistant, an AI that can help with a wide range of tasks, including " +
		"answering questions, writing content, generating code, translating languages, and more. " +
		"Always provide clear, concise, and helpful responses."
)

// ClientOption is a function that sets a Client option.
type ClientOption func(*Client)

func defaultClientOptions() *Client {
	return &Client{
		maxTokens:   defaultMaxTokens,
		temperature: 0.0,
	}
}

func (c *Client) apply(opts ...ClientOption) {
	for _, opt := range opts {
		opt(c)
	}
}

// WithMaxTokens sets the maximum number of tokens
func WithMaxTokens(max int) ClientOption {
	return func(c *Client) {
		if max < 256 {
			c.maxTokens = defaultMaxTokens
		}
		c.maxTokens = max
	}
}

// WithModel sets the model name
func WithModel(name string) ClientOption {
	return func(c *Client) {
		c.ModelName = name
	}
}

// WithTemperature sets the temperature
func WithTemperature(temperature float32) ClientOption {
	return func(c *Client) {
		c.temperature = temperature
	}
}

// WithRole sets the role type
func WithRole(role string) ClientOption {
	return func(c *Client) {
		c.role = openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: role,
		}
	}
}

// WithUseContext sets assistant context
func WithUseContext(isUse bool) ClientOption {
	return func(c *Client) {
		c.useContext = isUse
	}
}

// Client is a chat GPT client.
type Client struct {
	apiKey    string
	maxTokens int
	ModelName string
	Cli       *openai.Client

	/*
		| Temperature Value | Randomness   | Applicable Scenarios                      |
		|----------------|------------------------|-------------------------------------------|
		| 0                 | No randomness         | Factual answers, code generation, technical documentation |
		| 0.5 - 0.7      | Moderate randomness | Conversational systems, content creation, recommendation systems |
		| 1                 | High randomness       | Creative writing, brainstorming, advertising copy |
		| 1.5 - 2         | Extreme randomness  | Artistic creation, game design, exploratory tasks |
	*/
	temperature float32

	role openai.ChatCompletionMessage // default is general assistant

	useContext               bool                           // whether to use assistant context, default is false
	assistantContextMessages []openai.ChatCompletionMessage // assistant context
	mutex                    sync.Mutex                     // lock for assistant context
}

// NewClient creates a new chat client.
func NewClient(apiKey string, opts ...ClientOption) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("API key cannot be empty")
	}

	c := defaultClientOptions()
	c.apply(opts...)

	if c.ModelName == "" {
		c.ModelName = DefaultModel
	}
	c.apiKey = apiKey
	c.Cli = openai.NewClient(apiKey)

	return c, nil
}

// Send sends a prompt to the chat gpt and returns the response.
func (c *Client) Send(ctx context.Context, prompt string) (string, error) {
	if prompt == "" {
		return "", errors.New("prompt cannot be empty")
	}

	reply, err := c.Cli.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model:               c.ModelName,
			Messages:            c.getMessages(prompt),
			Temperature:         c.temperature,
			MaxCompletionTokens: c.maxTokens,
			MaxTokens:           c.maxTokens, // Deprecated
		},
	)
	if err != nil {
		return "", err
	}

	if len(reply.Choices) == 0 {
		return "", errors.New("empty response")
	}

	replyContent := reply.Choices[0].Message.Content
	c.setAssistantContext(replyContent)

	return replyContent, nil
}

// StreamReply reply with stream response
type StreamReply struct {
	Content chan string
	Err     error // if nil means successfully response
}

// SendStream sends a prompt to the chat gpt and returns a channel of responses.
func (c *Client) SendStream(ctx context.Context, prompt string) *StreamReply {
	response := &StreamReply{Content: make(chan string), Err: error(nil)}

	go func() {
		defer func() { close(response.Content) }()

		req := openai.ChatCompletionRequest{
			Model:               c.ModelName,
			Messages:            c.getMessages(prompt),
			Stream:              true,
			Temperature:         c.temperature,
			MaxCompletionTokens: c.maxTokens,
			MaxTokens:           c.maxTokens, // Deprecated
		}
		stream, err := c.Cli.CreateChatCompletionStream(ctx, req)
		if err != nil {
			response.Err = err
			return
		}
		defer func() {
			_ = stream.Close() //nolint
		}()

		var replyContent string
		defer func() {
			c.setAssistantContext(replyContent)
		}()

		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				response.Err = err
				return
			}

			for _, choice := range resp.Choices {
				select {
				case <-ctx.Done():
					response.Err = ctx.Err()
					return
				case response.Content <- choice.Delta.Content:
					replyContent += choice.Delta.Content
				}
			}
		}
	}()

	return response
}

// ListModelNames lists all available model names.
func (c *Client) ListModelNames(ctx context.Context) ([]string, error) {
	list, err := c.Cli.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	var modelNames []string
	for _, model := range list.Models {
		modelNames = append(modelNames, model.ID)
	}

	return modelNames, nil
}

func (c *Client) setAssistantContext(content string) {
	if c.useContext && len(content) > 0 {
		c.mutex.Lock()
		c.assistantContextMessages = append(c.assistantContextMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: content,
		})
		c.mutex.Unlock()
	}
}

// RefreshContext refreshes assistant context
func (c *Client) RefreshContext() {
	if len(c.assistantContextMessages) > 0 {
		c.mutex.Lock()
		c.assistantContextMessages = nil
		c.mutex.Unlock()
	}
}

func (c *Client) getMessages(prompt string) []openai.ChatCompletionMessage {
	var messages []openai.ChatCompletionMessage

	c.mutex.Lock()
	if len(c.assistantContextMessages) > 0 {
		messages = append(messages, c.assistantContextMessages...)
	}
	c.mutex.Unlock()

	if c.role.Content != "" {
		messages = append(messages, c.role)
	}

	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: prompt,
	})

	return messages
}
