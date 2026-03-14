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

// AnthropicProvider implements the Provider interface for Claude.
type AnthropicProvider struct {
	apiKey   string
	endpoint string
	model    string
	maxTokens int
	client   *http.Client
}

func NewAnthropicProvider(cfg config.ProviderConfig) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:    cfg.APIKey,
		endpoint:  cfg.Endpoint,
		model:     cfg.Model,
		maxTokens: cfg.MaxTokens,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *AnthropicProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	start := time.Now()

	maxTokens := p.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	body := anthropicRequest{
		Model:     p.model,
		MaxTokens: maxTokens,
		System:    req.SystemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: req.UserPrompt},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		p.endpoint+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if anthropicResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", anthropicResp.Error.Message)
	}

	content := ""
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &CompletionResponse{
		Content:      content,
		Provider:     "anthropic",
		Model:        p.model,
		InputTokens:  anthropicResp.Usage.InputTokens,
		OutputTokens: anthropicResp.Usage.OutputTokens,
		LatencyMs:    time.Since(start).Milliseconds(),
	}, nil
}

func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	// Simple health check — verify the API key is valid with a minimal request
	req := CompletionRequest{
		UserPrompt: "ping",
		MaxTokens:  1,
	}
	_, err := p.Complete(ctx, req)
	return err
}
