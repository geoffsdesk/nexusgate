package discovery

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// CapabilityManifest mirrors the Rust-side JSON-LD manifest.
type CapabilityManifest struct {
	Context      string                       `json:"@context"`
	Type         string                       `json:"@type"`
	Service      ServiceInfo                  `json:"service"`
	Capabilities []Capability                 `json:"capabilities"`
	Schemas      map[string]SchemaDefinition  `json:"schemas"`
}

type ServiceInfo struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Protocol    string   `json:"protocol"`
	BaseURL     string   `json:"base_url"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags"`
}

type Capability struct {
	Operation   string                     `json:"operation"`
	Description string                     `json:"description,omitempty"`
	Method      string                     `json:"method,omitempty"`
	Path        string                     `json:"path,omitempty"`
	Input       map[string]FieldDefinition `json:"input"`
	Output      json.RawMessage            `json:"output"`
	Idempotent  bool                       `json:"idempotent"`
	Cacheable   bool                       `json:"cacheable"`
}

type FieldDefinition struct {
	Type        string `json:"type"`
	Format      string `json:"format,omitempty"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

type SchemaDefinition struct {
	Type        string                     `json:"type"`
	Properties  map[string]FieldDefinition `json:"properties,omitempty"`
	Required    []string                   `json:"required,omitempty"`
	Description string                     `json:"description,omitempty"`
}

// Index holds all registered capability manifests and provides search.
type Index struct {
	manifests map[string]*CapabilityManifest // keyed by service ID
	mu        sync.RWMutex
}

func NewIndex() *Index {
	return &Index{
		manifests: make(map[string]*CapabilityManifest),
	}
}

// Register adds or updates a capability manifest.
func (idx *Index) Register(manifest *CapabilityManifest) string {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	id := uuid.New().String()
	idx.manifests[id] = manifest

	log.Info().
		Str("service", manifest.Service.Name).
		Int("capabilities", len(manifest.Capabilities)).
		Msg("Capability manifest registered")

	return id
}

// Search finds capabilities matching a natural language query.
// Uses simple keyword matching; can be upgraded to semantic search with embeddings.
func (idx *Index) Search(query string) []SearchResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	query = strings.ToLower(query)
	keywords := strings.Fields(query)
	var results []SearchResult

	for serviceID, manifest := range idx.manifests {
		for _, cap := range manifest.Capabilities {
			score := 0
			text := strings.ToLower(cap.Operation + " " + cap.Description)

			for _, kw := range keywords {
				if strings.Contains(text, kw) {
					score++
				}
			}

			// Also match against service name and tags
			serviceText := strings.ToLower(manifest.Service.Name + " " + strings.Join(manifest.Service.Tags, " "))
			for _, kw := range keywords {
				if strings.Contains(serviceText, kw) {
					score++
				}
			}

			if score > 0 {
				results = append(results, SearchResult{
					ServiceID:   serviceID,
					ServiceName: manifest.Service.Name,
					Capability:  cap,
					Score:       score,
				})
			}
		}
	}

	// Sort by relevance score (descending)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results
}

type SearchResult struct {
	ServiceID   string     `json:"service_id"`
	ServiceName string     `json:"service_name"`
	Capability  Capability `json:"capability"`
	Score       int        `json:"score"`
}

// ── HTTP Handlers ──

func (idx *Index) HandleList(w http.ResponseWriter, r *http.Request) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	type entry struct {
		ID       string              `json:"id"`
		Manifest *CapabilityManifest `json:"manifest"`
	}

	entries := make([]entry, 0, len(idx.manifests))
	for id, m := range idx.manifests {
		entries = append(entries, entry{ID: id, Manifest: m})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"services": entries,
		"total":    len(entries),
	})
}

func (idx *Index) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var manifest CapabilityManifest
	if err := json.NewDecoder(r.Body).Decode(&manifest); err != nil {
		http.Error(w, `{"error":"invalid manifest"}`, http.StatusBadRequest)
		return
	}

	id := idx.Register(&manifest)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func (idx *Index) HandleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, `{"error":"missing query parameter 'q'"}`, http.StatusBadRequest)
		return
	}

	results := idx.Search(query)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results,
		"total":   len(results),
		"query":   query,
	})
}

func (idx *Index) HandleGet(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceId")

	idx.mu.RLock()
	manifest, ok := idx.manifests[serviceID]
	idx.mu.RUnlock()

	if !ok {
		http.Error(w, `{"error":"service not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(manifest)
}

func (idx *Index) HandleDelete(w http.ResponseWriter, r *http.Request) {
	serviceID := chi.URLParam(r, "serviceId")

	idx.mu.Lock()
	_, ok := idx.manifests[serviceID]
	if ok {
		delete(idx.manifests, serviceID)
	}
	idx.mu.Unlock()

	if !ok {
		http.Error(w, `{"error":"service not found"}`, http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
