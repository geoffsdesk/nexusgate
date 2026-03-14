package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/nexusgate/ai-orchestrator/internal/config"
)

// CompletionRequest is a provider-agnostic LLM request.
type CompletionRequest struct {
	SystemPrompt string            `json:"system_prompt"`
	UserPrompt   string            `json:"user_prompt"`
	MaxTokens    int               `json:"max_tokens,omitempty"`
	Temperature  float64           `json:"temperature,omitempty"`
	JSONMode     bool              `json:"json_mode,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// CompletionResponse is a provider-agnostic LLM response.
type CompletionResponse struct {
	Content      string `json:"content"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	LatencyMs    int64  `json:"latency_ms"`
}

// Provider is the interface each LLM backend must implement.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	HealthCheck(ctx context.Context) error
}

// Router manages multiple LLM providers with priority-based failover.
type Router struct {
	providers []providerEntry
	mu        sync.RWMutex
}

type providerEntry struct {
	provider Provider
	priority int
	healthy  bool
}

// NewRouter initializes the LLM router with configured providers.
func NewRouter(cfg config.LLMConfig) (*Router, error) {
	router := &Router{}

	for _, pc := range cfg.Providers {
		if pc.APIKey == "" {
			log.Warn().Str("provider", pc.Name).Msg("Skipping provider (no API key)")
			continue
		}

		var p Provider
		switch pc.Type {
		case "anthropic":
			p = NewAnthropicProvider(pc)
		case "openai":
			p = NewOpenAIProvider(pc)
		default:
			log.Warn().Str("type", pc.Type).Msg("Unknown provider type, skipping")
			continue
		}

		router.providers = append(router.providers, providerEntry{
			provider: p,
			priority: pc.Priority,
			healthy:  true,
		})

		log.Info().
			Str("provider", pc.Name).
			Str("model", pc.Model).
			Int("priority", pc.Priority).
			Msg("LLM provider registered")
	}

	// Sort by priority
	sort.Slice(router.providers, func(i, j int) bool {
		return router.providers[i].priority < router.providers[j].priority
	})

	if len(router.providers) == 0 {
		log.Warn().Msg("No LLM providers configured — AI features will be unavailable")
	}

	return router, nil
}

// Complete sends a request to the highest-priority healthy provider.
// Falls back to lower-priority providers on failure.
func (r *Router) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var lastErr error
	for i := range r.providers {
		entry := &r.providers[i]
		if !entry.healthy {
			continue
		}

		resp, err := entry.provider.Complete(ctx, req)
		if err != nil {
			log.Warn().
				Err(err).
				Str("provider", entry.provider.Name()).
				Msg("LLM request failed, trying next provider")
			lastErr = err
			// Mark unhealthy (will be re-checked by health monitor)
			entry.healthy = false
			continue
		}

		return resp, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all LLM providers failed, last error: %w", lastErr)
	}
	return nil, fmt.Errorf("no LLM providers available")
}

// CompleteWithStructuredOutput sends a request and validates the response is valid JSON.
func (r *Router) CompleteWithStructuredOutput(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	req.JSONMode = true
	resp, err := r.Complete(ctx, req)
	if err != nil {
		return nil, err
	}

	// Validate JSON output
	var js json.RawMessage
	if err := json.Unmarshal([]byte(resp.Content), &js); err != nil {
		return nil, fmt.Errorf("LLM response is not valid JSON: %w", err)
	}

	return resp, nil
}

// HandleListProviders returns the status of all configured providers.
func (r *Router) HandleListProviders(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	type providerInfo struct {
		Name     string `json:"name"`
		Priority int    `json:"priority"`
		Healthy  bool   `json:"healthy"`
	}

	providers := make([]providerInfo, len(r.providers))
	for i, p := range r.providers {
		providers[i] = providerInfo{
			Name:     p.provider.Name(),
			Priority: p.priority,
			Healthy:  p.healthy,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"providers": providers,
		"total":     len(providers),
	})
}

// HandleHealthCheck runs health checks on all providers.
func (r *Router) HandleHealthCheck(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	r.mu.Lock()
	defer r.mu.Unlock()

	results := make(map[string]bool)
	for i := range r.providers {
		err := r.providers[i].provider.HealthCheck(ctx)
		healthy := err == nil
		r.providers[i].healthy = healthy
		results[r.providers[i].provider.Name()] = healthy
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}
