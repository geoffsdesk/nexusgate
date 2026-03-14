package store

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Contract represents a versioned API contract between a consumer and backend services.
type Contract struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Version     string       `json:"version"`
	Description string       `json:"description"`
	ConsumerID  string       `json:"consumer_id"`
	Status      string       `json:"status"` // "draft", "active", "deprecated", "revoked"
	Endpoints   []Endpoint   `json:"endpoints"`
	Types       []TypeDef    `json:"types"`
	Policies    []PolicyRef  `json:"policies"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	ExpiresAt   *time.Time   `json:"expires_at,omitempty"`
}

type Endpoint struct {
	Operation   string            `json:"operation"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Description string            `json:"description"`
	Input       map[string]string `json:"input"`
	Output      string            `json:"output"`
	RateLimit   *int              `json:"rate_limit,omitempty"`
	CacheMaxAge *int              `json:"cache_max_age,omitempty"`
}

type TypeDef struct {
	Name   string            `json:"name"`
	Fields map[string]string `json:"fields"`
}

type PolicyRef struct {
	PolicyID string `json:"policy_id"`
	Type     string `json:"type"` // "rbac", "rate_limit", "opa"
}

// Store is an in-memory contract store (production would use PostgreSQL).
type Store struct {
	contracts map[string]*Contract
	mu        sync.RWMutex
}

func New() *Store {
	return &Store{
		contracts: make(map[string]*Contract),
	}
}

func (s *Store) Create(c *Contract) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	c.ID = uuid.New().String()
	c.Status = "draft"
	c.CreatedAt = time.Now().UTC()
	c.UpdatedAt = c.CreatedAt
	s.contracts[c.ID] = c

	log.Info().Str("id", c.ID).Str("name", c.Name).Msg("Contract created")
	return c.ID
}

func (s *Store) Get(id string) (*Contract, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.contracts[id]
	return c, ok
}

func (s *Store) List() []*Contract {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Contract, 0, len(s.contracts))
	for _, c := range s.contracts {
		result = append(result, c)
	}
	return result
}

func (s *Store) Update(id string, updated *Contract) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.contracts[id]; !ok {
		return false
	}
	updated.ID = id
	updated.UpdatedAt = time.Now().UTC()
	s.contracts[id] = updated
	return true
}

func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.contracts[id]; !ok {
		return false
	}
	delete(s.contracts, id)
	return true
}

func (s *Store) Activate(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.contracts[id]
	if !ok {
		return false
	}
	c.Status = "active"
	c.UpdatedAt = time.Now().UTC()
	return true
}

// ── HTTP Handlers ──

func (s *Store) HandleList(w http.ResponseWriter, r *http.Request) {
	contracts := s.List()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"contracts": contracts,
		"total":     len(contracts),
	})
}

func (s *Store) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var contract Contract
	if err := json.NewDecoder(r.Body).Decode(&contract); err != nil {
		http.Error(w, `{"error":"invalid contract"}`, http.StatusBadRequest)
		return
	}
	id := s.Create(&contract)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (s *Store) HandleGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "contractId")
	contract, ok := s.Get(id)
	if !ok {
		http.Error(w, `{"error":"contract not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contract)
}

func (s *Store) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "contractId")
	var contract Contract
	if err := json.NewDecoder(r.Body).Decode(&contract); err != nil {
		http.Error(w, `{"error":"invalid contract"}`, http.StatusBadRequest)
		return
	}
	if !s.Update(id, &contract) {
		http.Error(w, `{"error":"contract not found"}`, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Store) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "contractId")
	if !s.Delete(id) {
		http.Error(w, `{"error":"contract not found"}`, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
