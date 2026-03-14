# ============================================================================
# NexusGate AI Orchestrator — Multi-stage Docker Build
# Consumer build: expects the nexusgate source repo as build context
# Usage: docker build -f docker/ai-orchestrator.Dockerfile ../nexusgate
# ============================================================================

# ── Stage 1: Build ──
FROM golang:1.22-bookworm AS builder

WORKDIR /build

# Copy go module files first for dependency caching
# Copy full source (needed for go mod tidy to resolve all imports)
COPY ai-orchestrator/ ./ai-orchestrator/

# Generate go.sum and download dependencies, then build
RUN cd ai-orchestrator && \
    go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/orchestrator ./cmd/orchestrator

# ── Stage 2: Runtime ──
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /build/orchestrator /usr/local/bin/orchestrator

ENV ORCHESTRATOR_PORT=8081

EXPOSE 8081

ENTRYPOINT ["orchestrator"]
