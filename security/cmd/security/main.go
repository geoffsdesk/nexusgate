package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/nexusgate/security/internal/auth"
	"github.com/nexusgate/security/internal/rbac"
	"github.com/nexusgate/security/internal/audit"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Info().Msg("Starting NexusGate Security Service")

	authenticator := auth.NewAuthenticator()
	rbacEngine := rbac.NewEngine()
	auditLogger := audit.NewLogger()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy","component":"security"}`))
	})

	// Token validation endpoint (called by gateway core)
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/validate", authenticator.HandleValidate)
		r.Post("/token", authenticator.HandleIssueToken)
		r.Get("/jwks", authenticator.HandleJWKS)
	})

	// RBAC management
	r.Route("/api/v1/rbac", func(r chi.Router) {
		r.Get("/roles", rbacEngine.HandleListRoles)
		r.Post("/roles", rbacEngine.HandleCreateRole)
		r.Post("/check", rbacEngine.HandleCheckPermission)
		r.Post("/assign", rbacEngine.HandleAssignRole)
	})

	// Audit log
	r.Route("/api/v1/audit", func(r chi.Router) {
		r.Get("/", auditLogger.HandleQuery)
		r.Post("/", auditLogger.HandleLog)
	})

	port := os.Getenv("SECURITY_PORT")
	if port == "" {
		port = "8083"
	}

	srv := &http.Server{Addr: fmt.Sprintf(":%s", port), Handler: r}

	go func() {
		log.Info().Str("addr", srv.Addr).Msg("Security Service listening")
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
