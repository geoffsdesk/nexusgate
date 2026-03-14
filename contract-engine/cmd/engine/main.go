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

	"github.com/nexusgate/contract-engine/internal/generator"
	"github.com/nexusgate/contract-engine/internal/snp"
	"github.com/nexusgate/contract-engine/internal/store"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Info().Msg("Starting NexusGate Contract Engine")

	contractStore := store.New()
	sdkGenerator := generator.New()
	snpHandler := snp.NewHandler(contractStore, sdkGenerator)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy","component":"contract-engine"}`))
	})

	// Contract CRUD
	r.Route("/api/v1/contracts", func(r chi.Router) {
		r.Get("/", contractStore.HandleList)
		r.Post("/", contractStore.HandleCreate)
		r.Get("/{contractId}", contractStore.HandleGet)
		r.Put("/{contractId}", contractStore.HandleUpdate)
		r.Delete("/{contractId}", contractStore.HandleDelete)
	})

	// SDK Generation
	r.Route("/api/v1/generate", func(r chi.Router) {
		r.Post("/typescript", sdkGenerator.HandleGenerateTS)
		r.Post("/python", sdkGenerator.HandleGeneratePython)
		r.Post("/go", sdkGenerator.HandleGenerateGo)
		r.Post("/openapi", sdkGenerator.HandleGenerateOpenAPI)
	})

	// Schema Negotiation Protocol (for AI agents)
	r.Route("/snp", func(r chi.Router) {
		r.Get("/capabilities", snpHandler.HandleDiscover)
		r.Post("/negotiate", snpHandler.HandleNegotiate)
		r.Post("/accept/{proposalId}", snpHandler.HandleAccept)
		r.Post("/counter/{proposalId}", snpHandler.HandleCounter)
	})

	port := os.Getenv("CONTRACT_ENGINE_PORT")
	if port == "" {
		port = "8082"
	}
	addr := fmt.Sprintf(":%s", port)

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		log.Info().Str("addr", addr).Msg("Contract Engine listening")
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
