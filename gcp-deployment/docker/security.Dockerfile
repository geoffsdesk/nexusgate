# ============================================================================
# NexusGate Security Service — Multi-stage Docker Build
# Consumer build: expects the nexusgate source repo as build context
# Usage: docker build -f docker/security.Dockerfile ../nexusgate
# ============================================================================

# ── Stage 1: Build ──
FROM golang:1.22-bookworm AS builder

WORKDIR /build

COPY security/ ./security/

RUN cd security && \
    go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/security ./cmd/security

# ── Stage 2: Runtime ──
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /build/security /usr/local/bin/security

ENV SECURITY_PORT=8083

EXPOSE 8083

ENTRYPOINT ["/usr/local/bin/security"]
