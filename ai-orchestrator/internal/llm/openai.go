package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nexusgate/ai-orchestrator/internal/config"
)

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs.
type OpenAIProvider struct {
	apiKey    string
	endpoint  string
	model     string
	maxTokens int
	client    *http.Client
}

func NewOpenAIProvider(cfg config.ProviderConfig) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey:    cfg.APIKey,
		endpoint:  cfg.Endpoint,
		model:     cfg.Model,
		maxTokens: cfg.MaxTokens,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *OpenAIProvider) Name() string {
	return "openai"
}

type openAIRequest struct {
	Model       string            `json:"model"`
	Messages    []openAIMessage   `json:"messages"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	start := time.Now()

	maxTokens := p.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	messages := []openAIMessage{}
	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, openAIMessage{Role: "user", Content: req.UserPrompt})

	body := openAIRequest{
		Model:     p.model,
		Messages:  messages,
		MaxTokens: maxTokens,
	}

	if req.JSONMode {
		body.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		p.endpoint+"/v1/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if oaiResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", oaiResp.Error.Message)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return &CompletionResponse{
		Content:      oaiResp.Choices[0].Message.Content,
		Provider:     "openai",
		Model:        p.model,
		InputTokens:  oaiResp.Usage.PromptTokens,
		OutputTokens: oaiResp.Usage.CompletionTokens,
		LatencyMs:    time.Since(start).Milliseconds(),
	}, nil
}

func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	req := CompletionRequest{
		UserPrompt: "ping",
		MaxTokens:  1,
	}
	_, err := p.Complete(ctx, req)
	return err
}
