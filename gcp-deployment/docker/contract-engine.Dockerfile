# ============================================================================
# NexusGate Contract Engine — Multi-stage Docker Build
# Consumer build: expects the nexusgate source repo as build context
# Usage: docker build -f docker/contract-engine.Dockerfile ../nexusgate
# ============================================================================

# ── Stage 1: Build ──
FROM golang:1.22-bookworm AS builder

WORKDIR /build

COPY contract-engine/ ./contract-engine/

RUN cd contract-engine && \
    go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/engine ./cmd/engine

# ── Stage 2: Runtime ──
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /build/engine /usr/local/bin/engine

ENV CONTRACT_ENGINE_PORT=8082

EXPOSE 8082

ENTRYPOINT ["engine"]
