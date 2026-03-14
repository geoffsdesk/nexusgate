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

	aiconfig "github.com/nexusgate/ai-orchestrator/internal/config"
	"github.com/nexusgate/ai-orchestrator/internal/discovery"
	"github.com/nexusgate/ai-orchestrator/internal/llm"
	"github.com/nexusgate/ai-orchestrator/internal/translator"
)

func main() {
	// Initialize structured logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Info().Msg("Starting NexusGate AI Orchestrator")

	// Load config
	cfg, err := aiconfig.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Initialize LLM router
	llmRouter, err := llm.NewRouter(cfg.LLM)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize LLM router")
	}

	// Initialize capability discovery index
	discoveryIndex := discovery.NewIndex()

	// Initialize NL→Contract translator
	nlTranslator := translator.New(llmRouter, discoveryIndex)

	// Set up HTTP API
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Health & readiness
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy","component":"ai-orchestrator"}`))
	})

	// ── Capability Discovery API ──
	r.Route("/api/v1/capabilities", func(r chi.Router) {
		r.Get("/", discoveryIndex.HandleList)
		r.Post("/register", discoveryIndex.HandleRegister)
		r.Get("/search", discoveryIndex.HandleSearch)
		r.Get("/{serviceId}", discoveryIndex.HandleGet)
		r.Delete("/{serviceId}", discoveryIndex.HandleDelete)
	})

	// ── NL Translation API ──
	r.Route("/api/v1/translate", func(r chi.Router) {
		r.Post("/", nlTranslator.HandleTranslate)
		r.Post("/suggest", nlTranslator.HandleSuggest)
	})

	// ── LLM Management API ──
	r.Route("/api/v1/llm", func(r chi.Router) {
		r.Get("/providers", llmRouter.HandleListProviders)
		r.Get("/health", llmRouter.HandleHealthCheck)
	})

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		log.Info().Str("addr", addr).Msg("AI Orchestrator listening")
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("Shutting down AI Orchestrator...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
