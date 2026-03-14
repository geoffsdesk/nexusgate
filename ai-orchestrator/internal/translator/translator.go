package translator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/nexusgate/ai-orchestrator/internal/discovery"
	"github.com/nexusgate/ai-orchestrator/internal/llm"
)

// Translator converts natural language descriptions into contract specifications.
// This is the core "generative AI" component of NexusGate.
type Translator struct {
	llmRouter *llm.Router
	index     *discovery.Index
}

func New(router *llm.Router, index *discovery.Index) *Translator {
	return &Translator{
		llmRouter: router,
		index:     index,
	}
}

// TranslateRequest is what a consumer sends to describe what they need.
type TranslateRequest struct {
	Description string   `json:"description"` // NL description like "I need user CRUD with pagination"
	Constraints []string `json:"constraints,omitempty"` // e.g. ["read-only", "no PII fields"]
	OutputFormat string  `json:"output_format,omitempty"` // "openapi", "graphql", "typescript"
}

// ContractSpec is the structured output the LLM produces.
type ContractSpec struct {
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description"`
	Endpoints   []EndpointSpec `json:"endpoints"`
	Types       []TypeSpec   `json:"types"`
	Constraints []string     `json:"constraints_applied"`
}

type EndpointSpec struct {
	Operation   string            `json:"operation"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Description string            `json:"description"`
	Input       map[string]string `json:"input"`
	Output      string            `json:"output"`
	Source      SourceMapping     `json:"source"`
}

type SourceMapping struct {
	ServiceName string `json:"service_name"`
	ServiceID   string `json:"service_id"`
	Operation   string `json:"operation"`
}

type TypeSpec struct {
	Name       string            `json:"name"`
	Fields     map[string]string `json:"fields"`
}

const systemPrompt = `You are NexusGate's contract translator. Your job is to convert natural language API requirements into structured contract specifications.

You will receive:
1. A natural language description of what the consumer needs
2. Available backend capabilities (as a JSON capability index)
3. Any constraints the consumer has specified

You must produce a JSON contract specification that:
- Maps each requested capability to an available backend operation
- Defines clean, consumer-friendly endpoint paths
- Creates appropriate request/response types
- Respects all constraints (read-only, field filtering, etc.)
- Uses RESTful conventions for paths and methods

Output ONLY valid JSON matching this schema:
{
  "name": "string",
  "version": "string",
  "description": "string",
  "endpoints": [
    {
      "operation": "string",
      "method": "GET|POST|PUT|DELETE|PATCH",
      "path": "/api/v1/...",
      "description": "string",
      "input": {"param_name": "type"},
      "output": "TypeName",
      "source": {
        "service_name": "string",
        "service_id": "string",
        "operation": "string"
      }
    }
  ],
  "types": [
    {
      "name": "string",
      "fields": {"field_name": "type"}
    }
  ],
  "constraints_applied": ["string"]
}`

// Translate converts a natural language request into a contract specification.
func (t *Translator) Translate(ctx context.Context, req TranslateRequest) (*ContractSpec, error) {
	// Search for relevant capabilities
	results := t.index.Search(req.Description)

	// Build context for the LLM
	capabilitiesJSON, _ := json.MarshalIndent(results, "", "  ")

	userPrompt := fmt.Sprintf(`Consumer request: %s

Constraints: %v

Available backend capabilities:
%s

Generate a contract specification that fulfills this request using the available capabilities.`,
		req.Description, req.Constraints, string(capabilitiesJSON))

	// Call LLM with structured output
	llmResp, err := t.llmRouter.CompleteWithStructuredOutput(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    4096,
		Temperature:  0.1, // Low temperature for deterministic contracts
		JSONMode:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM translation failed: %w", err)
	}

	// Parse the structured output
	var spec ContractSpec
	if err := json.Unmarshal([]byte(llmResp.Content), &spec); err != nil {
		return nil, fmt.Errorf("failed to parse LLM output as contract spec: %w", err)
	}

	log.Info().
		Str("name", spec.Name).
		Int("endpoints", len(spec.Endpoints)).
		Int("types", len(spec.Types)).
		Str("provider", llmResp.Provider).
		Int64("latency_ms", llmResp.LatencyMs).
		Msg("Contract translated from natural language")

	return &spec, nil
}

// Suggest returns capability suggestions for a partial description.
func (t *Translator) Suggest(ctx context.Context, partial string) []discovery.SearchResult {
	return t.index.Search(partial)
}

// ── HTTP Handlers ──

func (t *Translator) HandleTranslate(w http.ResponseWriter, r *http.Request) {
	var req TranslateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if req.Description == "" {
		http.Error(w, `{"error":"description is required"}`, http.StatusBadRequest)
		return
	}

	spec, err := t.Translate(r.Context(), req)
	if err != nil {
		log.Error().Err(err).Msg("Translation failed")
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec)
}

func (t *Translator) HandleSuggest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	results := t.Suggest(r.Context(), req.Query)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"suggestions": results,
		"total":       len(results),
	})
}
