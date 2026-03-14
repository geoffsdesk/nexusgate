package rbac

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Role defines a set of permissions that can be assigned to consumers.
type Role struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Permissions []Permission `json:"permissions"`
}

// Permission defines access to a specific resource/action.
type Permission struct {
	Resource string `json:"resource"` // e.g. "contracts:*", "routes:user-service"
	Actions  []string `json:"actions"` // e.g. ["read", "write", "delete"]
}

// Assignment binds a consumer to a role, optionally scoped to a contract.
type Assignment struct {
	ConsumerID string `json:"consumer_id"`
	RoleID     string `json:"role_id"`
	ContractID string `json:"contract_id,omitempty"` // Empty = global
}

// Engine manages roles, permissions, and access checks.
type Engine struct {
	roles       map[string]*Role
	assignments []Assignment
	mu          sync.RWMutex
}

func NewEngine() *Engine {
	e := &Engine{
		roles:       make(map[string]*Role),
		assignments: []Assignment{},
	}

	// Seed default roles
	e.roles["admin"] = &Role{
		ID:          "admin",
		Name:        "Administrator",
		Description: "Full access to all resources",
		Permissions: []Permission{
			{Resource: "*", Actions: []string{"*"}},
		},
	}
	e.roles["consumer"] = &Role{
		ID:          "consumer",
		Name:        "API Consumer",
		Description: "Access to contracted resources only",
		Permissions: []Permission{
			{Resource: "contracts:own", Actions: []string{"read"}},
			{Resource: "routes:contracted", Actions: []string{"invoke"}},
		},
	}
	e.roles["readonly"] = &Role{
		ID:          "readonly",
		Name:        "Read Only",
		Description: "Read access to capabilities and contracts",
		Permissions: []Permission{
			{Resource: "capabilities:*", Actions: []string{"read"}},
			{Resource: "contracts:own", Actions: []string{"read"}},
		},
	}

	return e
}

// CheckPermission verifies if a consumer has access to a resource/action.
func (e *Engine) CheckPermission(consumerID, resource, action string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, assignment := range e.assignments {
		if assignment.ConsumerID != consumerID {
			continue
		}

		role, ok := e.roles[assignment.RoleID]
		if !ok {
			continue
		}

		for _, perm := range role.Permissions {
			if matchResource(perm.Resource, resource) && matchAction(perm.Actions, action) {
				return true
			}
		}
	}

	return false
}

func matchResource(pattern, resource string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == resource {
		return true
	}
	// Simple wildcard: "contracts:*" matches "contracts:123"
	if len(pattern) > 1 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(resource) >= len(prefix) && resource[:len(prefix)] == prefix
	}
	return false
}

func matchAction(allowed []string, action string) bool {
	for _, a := range allowed {
		if a == "*" || a == action {
			return true
		}
	}
	return false
}

// ── HTTP Handlers ──

type CheckRequest struct {
	ConsumerID string `json:"consumer_id"`
	Resource   string `json:"resource"`
	Action     string `json:"action"`
}

type AssignRequest struct {
	ConsumerID string `json:"consumer_id"`
	RoleID     string `json:"role_id"`
	ContractID string `json:"contract_id,omitempty"`
}

func (e *Engine) HandleListRoles(w http.ResponseWriter, r *http.Request) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	roles := make([]*Role, 0, len(e.roles))
	for _, role := range e.roles {
		roles = append(roles, role)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"roles": roles})
}

func (e *Engine) HandleCreateRole(w http.ResponseWriter, r *http.Request) {
	var role Role
	if err := json.NewDecoder(r.Body).Decode(&role); err != nil {
		http.Error(w, `{"error":"invalid role"}`, http.StatusBadRequest)
		return
	}

	role.ID = uuid.New().String()

	e.mu.Lock()
	e.roles[role.ID] = &role
	e.mu.Unlock()

	log.Info().Str("role", role.Name).Msg("Role created")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": role.ID})
}

func (e *Engine) HandleCheckPermission(w http.ResponseWriter, r *http.Request) {
	var req CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	allowed := e.CheckPermission(req.ConsumerID, req.Resource, req.Action)

	w.Header().Set("Content-Type", "application/json")
	if !allowed {
		w.WriteHeader(http.StatusForbidden)
	}
	json.NewEncoder(w).Encode(map[string]bool{"allowed": allowed})
}

func (e *Engine) HandleAssignRole(w http.ResponseWriter, r *http.Request) {
	var req AssignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	e.mu.Lock()
	e.assignments = append(e.assignments, Assignment{
		ConsumerID: req.ConsumerID,
		RoleID:     req.RoleID,
		ContractID: req.ContractID,
	})
	e.mu.Unlock()

	log.Info().
		Str("consumer", req.ConsumerID).
		Str("role", req.RoleID).
		Msg("Role assigned")

	w.WriteHeader(http.StatusNoContent)
}
