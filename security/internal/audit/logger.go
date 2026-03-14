package audit

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Entry represents a single audit log event.
type Entry struct {
	ID         string                 `json:"id"`
	Timestamp  time.Time              `json:"timestamp"`
	ConsumerID string                 `json:"consumer_id"`
	Action     string                 `json:"action"`
	Resource   string                 `json:"resource"`
	ContractID string                 `json:"contract_id,omitempty"`
	RouteID    string                 `json:"route_id,omitempty"`
	Status     string                 `json:"status"` // "allowed", "denied", "error"
	Details    map[string]interface{} `json:"details,omitempty"`
	SourceIP   string                 `json:"source_ip,omitempty"`
}

// Logger stores and queries audit entries.
// In production, this writes to PostgreSQL or an external SIEM.
type Logger struct {
	entries []Entry
	mu      sync.RWMutex
}

func NewLogger() *Logger {
	return &Logger{
		entries: make([]Entry, 0),
	}
}

func (l *Logger) Log(entry Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry.ID = uuid.New().String()
	entry.Timestamp = time.Now().UTC()
	l.entries = append(l.entries, entry)

	// Keep last 10000 entries in memory
	if len(l.entries) > 10000 {
		l.entries = l.entries[len(l.entries)-10000:]
	}
}

func (l *Logger) Query(consumerID, action string, limit int) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	var results []Entry
	for i := len(l.entries) - 1; i >= 0 && len(results) < limit; i-- {
		e := l.entries[i]
		if (consumerID == "" || e.ConsumerID == consumerID) &&
			(action == "" || e.Action == action) {
			results = append(results, e)
		}
	}
	return results
}

// ── HTTP Handlers ──

func (l *Logger) HandleLog(w http.ResponseWriter, r *http.Request) {
	var entry Entry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		http.Error(w, `{"error":"invalid entry"}`, http.StatusBadRequest)
		return
	}
	l.Log(entry)
	w.WriteHeader(http.StatusCreated)
}

func (l *Logger) HandleQuery(w http.ResponseWriter, r *http.Request) {
	consumerID := r.URL.Query().Get("consumer_id")
	action := r.URL.Query().Get("action")

	entries := l.Query(consumerID, action, 100)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
		"total":   len(entries),
	})
}
