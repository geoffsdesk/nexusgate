package auth

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Authenticator validates tokens and issues JWTs for consumer authentication.
type Authenticator struct {
	signingKey []byte
	issuer     string
}

func NewAuthenticator() *Authenticator {
	// In production, load from config/secrets manager
	key := make([]byte, 32)
	rand.Read(key)

	return &Authenticator{
		signingKey: key,
		issuer:     "nexusgate",
	}
}

type ValidateRequest struct {
	Token      string `json:"token"`
	ContractID string `json:"contract_id,omitempty"`
}

type ValidateResponse struct {
	Valid      bool              `json:"valid"`
	ConsumerID string           `json:"consumer_id,omitempty"`
	Claims     map[string]interface{} `json:"claims,omitempty"`
	Error      string           `json:"error,omitempty"`
}

type TokenRequest struct {
	ConsumerID string   `json:"consumer_id"`
	Scopes     []string `json:"scopes"`
	ContractID string   `json:"contract_id,omitempty"`
	ExpiresIn  int      `json:"expires_in_seconds,omitempty"` // Default: 3600
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	ConsumerID  string `json:"consumer_id"`
}

// HandleValidate validates an incoming token.
func (a *Authenticator) HandleValidate(w http.ResponseWriter, r *http.Request) {
	var req ValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	// Try JWT validation first
	resp := a.validateJWT(req.Token)

	// If a contract_id is specified, verify the token is bound to that contract
	if resp.Valid && req.ContractID != "" {
		if claims, ok := resp.Claims["contract_id"]; ok {
			if claims != req.ContractID {
				resp.Valid = false
				resp.Error = "token not bound to this contract"
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if !resp.Valid {
		w.WriteHeader(http.StatusUnauthorized)
	}
	json.NewEncoder(w).Encode(resp)
}

func (a *Authenticator) validateJWT(tokenStr string) ValidateResponse {
	// Strip "Bearer " prefix if present
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.signingKey, nil
	})

	if err != nil {
		return ValidateResponse{Valid: false, Error: err.Error()}
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return ValidateResponse{Valid: false, Error: "invalid token claims"}
	}

	consumerID, _ := claims["sub"].(string)

	return ValidateResponse{
		Valid:      true,
		ConsumerID: consumerID,
		Claims:     claims,
	}
}

// HandleIssueToken creates a new JWT for a consumer.
func (a *Authenticator) HandleIssueToken(w http.ResponseWriter, r *http.Request) {
	var req TokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if req.ConsumerID == "" {
		http.Error(w, `{"error":"consumer_id is required"}`, http.StatusBadRequest)
		return
	}

	expiresIn := req.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 3600
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":         a.issuer,
		"sub":         req.ConsumerID,
		"aud":         "nexusgate",
		"exp":         now.Add(time.Duration(expiresIn) * time.Second).Unix(),
		"iat":         now.Unix(),
		"jti":         uuid.New().String(),
		"scopes":      req.Scopes,
		"contract_id": req.ContractID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(a.signingKey)
	if err != nil {
		log.Error().Err(err).Msg("Failed to sign token")
		http.Error(w, `{"error":"failed to issue token"}`, http.StatusInternalServerError)
		return
	}

	log.Info().
		Str("consumer", req.ConsumerID).
		Strs("scopes", req.Scopes).
		Msg("Token issued")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken: tokenStr,
		TokenType:   "Bearer",
		ExpiresIn:   expiresIn,
		ConsumerID:  req.ConsumerID,
	})
}

// HandleJWKS returns the JSON Web Key Set (for external OIDC validation).
func (a *Authenticator) HandleJWKS(w http.ResponseWriter, r *http.Request) {
	// Placeholder — in production, expose RSA/EC public keys
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"keys":[],"note":"HMAC signing in dev mode; production uses RSA/EC"}`))
}
