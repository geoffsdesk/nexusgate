package snp

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/nexusgate/contract-engine/internal/generator"
	"github.com/nexusgate/contract-engine/internal/store"
)

// Handler implements the Schema Negotiation Protocol (SNP).
// SNP allows AI agents to discover capabilities and negotiate contracts programmatically.
//
// Protocol flow:
//   Agent → DISCOVER → Capability list
//   Agent → NEGOTIATE → Sends needs/constraints → Gateway proposes contract
//   Agent → ACCEPT/COUNTER → Contract finalized
//   Agent → Uses contract → Requests flow through gateway

type Handler struct {
	store     *store.Store
	generator *generator.Generator
	proposals map[string]*Proposal
	mu        sync.RWMutex
}

type Proposal struct {
	ID        string          `json:"id"`
	Contract  *store.Contract `json:"contract"`
	Status    string          `json:"status"` // "pending", "accepted", "countered", "expired"
	ExpiresAt time.Time       `json:"expires_at"`
	CreatedAt time.Time       `json:"created_at"`
}

type NegotiateRequest struct {
	ConsumerID  string            `json:"consumer_id"`
	Needs       []NeedSpec        `json:"needs"`
	Constraints []string          `json:"constraints"`
	Preferences map[string]string `json:"preferences,omitempty"`
}

type NeedSpec struct {
	Operation   string            `json:"operation"`   // e.g. "read_users", "create_order"
	Description string            `json:"description"` // NL description for the AI layer
	Fields      []string          `json:"fields,omitempty"` // Specific fields needed
	Filters     map[string]string `json:"filters,omitempty"`
}

type CounterRequest struct {
	Modifications []Modification `json:"modifications"`
}

type Modification struct {
	EndpointIndex int               `json:"endpoint_index"`
	Changes       map[string]string `json:"changes"`
}

func NewHandler(store *store.Store, gen *generator.Generator) *Handler {
	return &Handler{
		store:     store,
		generator: gen,
		proposals: make(map[string]*Proposal),
	}
}

// HandleDiscover returns all available capabilities for agent discovery.
// SNP Step 1: DISCOVER
func (h *Handler) HandleDiscover(w http.ResponseWriter, r *http.Request) {
	// In production, this queries the AI orchestrator's capability index.
	// For now, return the SNP protocol description.
	response := map[string]interface{}{
		"protocol":    "SNP/1.0",
		"description": "NexusGate Schema Negotiation Protocol",
		"endpoints": map[string]string{
			"discover":  "GET /snp/capabilities",
			"negotiate": "POST /snp/negotiate",
			"accept":    "POST /snp/accept/{proposalId}",
			"counter":   "POST /snp/counter/{proposalId}",
		},
		"mcp_compatible": true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleNegotiate processes a contract negotiation request.
// SNP Step 2: NEGOTIATE
func (h *Handler) HandleNegotiate(w http.ResponseWriter, r *http.Request) {
	var req NegotiateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid negotiation request"}`, http.StatusBadRequest)
		return
	}

	// Build a contract proposal from the agent's needs
	contract := &store.Contract{
		Name:        "agent-contract-" + uuid.New().String()[:8],
		Version:     "1.0.0",
		Description: "Auto-negotiated contract via SNP",
		ConsumerID:  req.ConsumerID,
		Status:      "draft",
	}

	// Map needs to endpoints
	for _, need := range req.Needs {
		ep := store.Endpoint{
			Operation:   need.Operation,
			Method:      inferMethod(need.Operation),
			Path:        inferPath(need.Operation),
			Description: need.Description,
			Input:       make(map[string]string),
			Output:      inferOutputType(need.Operation),
		}

		// Add field filters as input params
		for k, v := range need.Filters {
			ep.Input[k] = v
		}

		contract.Endpoints = append(contract.Endpoints, ep)
	}

	// Create proposal
	proposal := &Proposal{
		ID:        uuid.New().String(),
		Contract:  contract,
		Status:    "pending",
		ExpiresAt: time.Now().Add(15 * time.Minute),
		CreatedAt: time.Now(),
	}

	h.mu.Lock()
	h.proposals[proposal.ID] = proposal
	h.mu.Unlock()

	log.Info().
		Str("proposal_id", proposal.ID).
		Str("consumer", req.ConsumerID).
		Int("endpoints", len(contract.Endpoints)).
		Msg("SNP proposal created")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(proposal)
}

// HandleAccept finalizes a proposed contract.
// SNP Step 3a: ACCEPT
func (h *Handler) HandleAccept(w http.ResponseWriter, r *http.Request) {
	proposalID := chi.URLParam(r, "proposalId")

	h.mu.Lock()
	proposal, ok := h.proposals[proposalID]
	if !ok {
		h.mu.Unlock()
		http.Error(w, `{"error":"proposal not found"}`, http.StatusNotFound)
		return
	}

	if proposal.Status != "pending" {
		h.mu.Unlock()
		http.Error(w, `{"error":"proposal is no longer pending"}`, http.StatusConflict)
		return
	}

	if time.Now().After(proposal.ExpiresAt) {
		proposal.Status = "expired"
		h.mu.Unlock()
		http.Error(w, `{"error":"proposal has expired"}`, http.StatusGone)
		return
	}

	proposal.Status = "accepted"
	h.mu.Unlock()

	// Store the finalized contract
	contractID := h.store.Create(proposal.Contract)
	h.store.Activate(contractID)

	log.Info().
		Str("proposal_id", proposalID).
		Str("contract_id", contractID).
		Msg("SNP contract accepted and activated")

	// Return the finalized contract with SDK generation endpoints
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "accepted",
		"contract_id": contractID,
		"contract":    proposal.Contract,
		"sdk_endpoints": map[string]string{
			"typescript": "/api/v1/generate/typescript",
			"python":     "/api/v1/generate/python",
			"openapi":    "/api/v1/generate/openapi",
		},
	})
}

// HandleCounter allows the agent to propose modifications.
// SNP Step 3b: COUNTER
func (h *Handler) HandleCounter(w http.ResponseWriter, r *http.Request) {
	proposalID := chi.URLParam(r, "proposalId")

	var counterReq CounterRequest
	if err := json.NewDecoder(r.Body).Decode(&counterReq); err != nil {
		http.Error(w, `{"error":"invalid counter request"}`, http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	proposal, ok := h.proposals[proposalID]
	if !ok {
		h.mu.Unlock()
		http.Error(w, `{"error":"proposal not found"}`, http.StatusNotFound)
		return
	}

	// Apply modifications
	for _, mod := range counterReq.Modifications {
		if mod.EndpointIndex >= 0 && mod.EndpointIndex < len(proposal.Contract.Endpoints) {
			ep := &proposal.Contract.Endpoints[mod.EndpointIndex]
			for key, value := range mod.Changes {
				switch key {
				case "method":
					ep.Method = value
				case "path":
					ep.Path = value
				case "description":
					ep.Description = value
				}
			}
		}
	}

	// Create new proposal with modifications
	newProposal := &Proposal{
		ID:        uuid.New().String(),
		Contract:  proposal.Contract,
		Status:    "pending",
		ExpiresAt: time.Now().Add(15 * time.Minute),
		CreatedAt: time.Now(),
	}

	proposal.Status = "countered"
	h.proposals[newProposal.ID] = newProposal
	h.mu.Unlock()

	log.Info().
		Str("old_proposal", proposalID).
		Str("new_proposal", newProposal.ID).
		Msg("SNP counter-proposal created")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newProposal)
}

// ── Helpers ──

func inferMethod(operation string) string {
	op := operation
	if len(op) > 0 {
		switch {
		case contains(op, "create", "add", "insert", "post"):
			return "POST"
		case contains(op, "update", "modify", "edit", "put"):
			return "PUT"
		case contains(op, "delete", "remove", "destroy"):
			return "DELETE"
		case contains(op, "patch", "partial"):
			return "PATCH"
		default:
			return "GET"
		}
	}
	return "GET"
}

func inferPath(operation string) string {
	// Convert operation_name to /api/v1/resource path
	parts := splitOperation(operation)
	if len(parts) >= 2 {
		return "/api/v1/" + parts[len(parts)-1]
	}
	return "/api/v1/" + operation
}

func inferOutputType(operation string) string {
	parts := splitOperation(operation)
	if len(parts) >= 2 {
		return toPascalCase(parts[len(parts)-1])
	}
	return "Response"
}

func splitOperation(op string) []string {
	var parts []string
	current := ""
	for _, r := range op {
		if r == '_' || r == '-' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func toPascalCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(s[0]-32) + s[1:]
}

func contains(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
