package openai

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/appleboy/CodeGPT/core"
	"github.com/appleboy/CodeGPT/core/transport"
	"github.com/appleboy/CodeGPT/proxy"
	"github.com/appleboy/CodeGPT/version"

	openai "github.com/sashabaranov/go-openai"
)

// DefaultModel is the default OpenAI model to use if one is not provided.
var DefaultModel = openai.GPT4o

var _ core.Generative = (*Client)(nil)

// Client is a struct that represents an OpenAI client.
type Client struct {
	client           *openai.Client
	model            string
	maxTokens        int
	temperature      float32
	topP             float32
	frequencyPenalty float32
	presencePenalty  float32
}

type Response struct {
	Content string
	Usage   openai.Usage
}

// Completion is a method on the Client struct that takes a context.Context and a string argument
func (c *Client) Completion(ctx context.Context, content string) (*core.Response, error) {
	resp, err := c.completion(ctx, content)
	if err != nil {
		return nil, err
	}

	return &core.Response{
		Content: resp.Content,
		Usage: core.Usage{
			PromptTokens:            resp.Usage.PromptTokens,
			CompletionTokens:        resp.Usage.CompletionTokens,
			TotalTokens:             resp.Usage.TotalTokens,
			CompletionTokensDetails: resp.Usage.CompletionTokensDetails,
		},
	}, nil
}

// GetSummaryPrefix is an API call to get a summary prefix using function call.
func (c *Client) GetSummaryPrefix(ctx context.Context, content string) (*core.Response, error) {
	var resp openai.ChatCompletionResponse
	var err error
	if checkOSeriesModels.MatchString(c.model) || strings.Contains(strings.ToLower(c.model), "deepseek") {
		resp, err = c.CreateChatCompletion(ctx, content)
		if err != nil || len(resp.Choices) != 1 {
			return nil, err
		}
	} else {
		resp, err = c.CreateFunctionCall(ctx, content, SummaryPrefixFunc)
		if err != nil || len(resp.Choices) != 1 {
			return nil, err
		}
	}

	msg := resp.Choices[0].Message
	usage := core.Usage{
		PromptTokens:            resp.Usage.PromptTokens,
		CompletionTokens:        resp.Usage.CompletionTokens,
		TotalTokens:             resp.Usage.TotalTokens,
		CompletionTokensDetails: resp.Usage.CompletionTokensDetails,
	}
	if len(msg.ToolCalls) == 0 {
		return &core.Response{
			Content: msg.Content,
			Usage:   usage,
		}, nil
	}

	args := GetSummaryPrefixArgs(msg.ToolCalls[len(msg.ToolCalls)-1].Function.Arguments)
	return &core.Response{
		Content: args.Prefix,
		Usage:   usage,
	}, nil
}

var checkOSeriesModels = regexp.MustCompile(`o\d(-(mini|preview))?`)

// CreateChatCompletion is an API call to create a function call for a chat message.
func (c *Client) CreateFunctionCall(
	ctx context.Context,
	content string,
	f openai.FunctionDefinition,
) (resp openai.ChatCompletionResponse, err error) {
	t := openai.Tool{
		Type:     openai.ToolTypeFunction,
		Function: &f,
	}

	req := openai.ChatCompletionRequest{
		Model:            c.model,
		MaxTokens:        c.maxTokens,
		Temperature:      c.temperature,
		TopP:             c.topP,
		FrequencyPenalty: c.frequencyPenalty,
		PresencePenalty:  c.presencePenalty,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleAssistant,
				Content: "You are a helpful assistant.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: content,
			},
		},
		Tools: []openai.Tool{t},
		ToolChoice: openai.ToolChoice{
			Type: openai.ToolTypeFunction,
			Function: openai.ToolFunction{
				Name: f.Name,
			},
		},
	}

	if checkOSeriesModels.MatchString(c.model) {
		req.MaxTokens = 0
		req.MaxCompletionTokens = c.maxTokens
	}

	return c.client.CreateChatCompletion(ctx, req)
}

// CreateChatCompletion is an API call to create a completion for a chat message.
func (c *Client) CreateChatCompletion(
	ctx context.Context,
	content string,
) (resp openai.ChatCompletionResponse, err error) {
	req := openai.ChatCompletionRequest{
		Model:            c.model,
		MaxTokens:        c.maxTokens,
		Temperature:      c.temperature,
		TopP:             c.topP,
		FrequencyPenalty: c.frequencyPenalty,
		PresencePenalty:  c.presencePenalty,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleAssistant,
				Content: "You are a helpful assistant.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: content,
			},
		},
	}

	if checkOSeriesModels.MatchString(c.model) {
		req.MaxTokens = 0
		req.MaxCompletionTokens = c.maxTokens
	}

	return c.client.CreateChatCompletion(ctx, req)
}

// Completion is a method on the Client struct that takes a context.Context and a string argument
// and returns a string and an error.
func (c *Client) completion(
	ctx context.Context,
	content string,
) (*Response, error) {
	resp := &Response{}
	r, err := c.CreateChatCompletion(ctx, content)
	if err != nil {
		return nil, err
	}
	if len(r.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned from API")
	}
	// Do not support reasoning content for now.
	if r.Choices[0].Message.ReasoningContent != "" {
		return nil, fmt.Errorf("reasoning model is not supported")
	}
	resp.Content = r.Choices[0].Message.Content
	resp.Usage = r.Usage
	return resp, nil
}

// New creates a new OpenAI API client with the given options.
func New(opts ...Option) (*Client, error) {
	// Create a new config object with the given options.
	cfg := newConfig(opts...)

	// Validate the config object, returning an error if it is invalid.
	if err := cfg.valid(); err != nil {
		return nil, err
	}

	// Create a new client instance with the necessary fields.
	engine := &Client{
		model:            cfg.model,
		maxTokens:        cfg.maxTokens,
		temperature:      cfg.temperature,
		topP:             cfg.topP,
		frequencyPenalty: cfg.frequencyPenalty,
		presencePenalty:  cfg.presencePenalty,
	}

	// Create a new OpenAI config object with the given API token and other optional fields.
	c := openai.DefaultConfig(cfg.token)
	if cfg.orgID != "" {
		c.OrgID = cfg.orgID
	}
	if cfg.baseURL != "" {
		c.BaseURL = cfg.baseURL
	}

	httpClient, err := proxy.New(
		proxy.WithProxyURL(cfg.proxyURL),
		proxy.WithSocksURL(cfg.socksURL),
		proxy.WithSkipVerify(cfg.skipVerify),
		proxy.WithTimeout(cfg.timeout),
		proxy.WithHeaders(cfg.headers),
	)
	if err != nil {
		return nil, fmt.Errorf("can't create a new HTTP client: %w", err)
	}

	// Inject x-app-name and x-app-version headers using core/transport.DefaultHeaderTransport
	// Always wrap the proxy's httpClient.Transport
	httpClient.Transport = &transport.DefaultHeaderTransport{
		Origin:     httpClient.Transport,
		Header:     nil,
		AppName:    version.App,
		AppVersion: version.Version,
	}

	// Set the OpenAI client to use the default configuration with Azure-specific options, if the provider is Azure.
	if cfg.provider == core.Azure {
		defaultAzureConfig := openai.DefaultAzureConfig(cfg.token, cfg.baseURL)
		defaultAzureConfig.AzureModelMapperFunc = func(model string) string {
			return cfg.model
		}
		// Set the API version to the one with the specified options.
		if cfg.apiVersion != "" {
			defaultAzureConfig.APIVersion = cfg.apiVersion
		}
		// Set the HTTP client to the one with the specified options.
		defaultAzureConfig.HTTPClient = httpClient
		engine.client = openai.NewClientWithConfig(
			defaultAzureConfig,
		)
	} else {
		// Otherwise, set the OpenAI client to use the HTTP client with the specified options.
		c.HTTPClient = httpClient
		if cfg.apiVersion != "" {
			c.APIVersion = cfg.apiVersion
		}

		engine.client = openai.NewClientWithConfig(c)
	}

	// Return the resulting client engine.
	return engine, nil
}
