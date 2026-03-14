# ============================================================================
# NexusGate Gateway Core — Multi-stage Docker Build
# Consumer build: expects the nexusgate source repo as build context
# Usage: docker build -f docker/gateway-core.Dockerfile ../nexusgate
# ============================================================================

# ── Stage 1: Build ──
FROM rust:1.85-bookworm AS builder

WORKDIR /build

# Copy just the manifest first for dependency caching
COPY gateway-core/Cargo.toml gateway-core/Cargo.lock* ./gateway-core/

# Create a dummy main.rs so cargo can resolve dependencies
RUN mkdir -p gateway-core/src && \
    echo 'fn main() { println!("placeholder"); }' > gateway-core/src/main.rs

# Pre-build dependencies (cached layer)
RUN cd gateway-core && cargo build --release 2>/dev/null || true

# Now copy the real source
COPY gateway-core/src ./gateway-core/src
COPY gateway-core/build.rs* ./gateway-core/
COPY gateway-core/proto* ./gateway-core/proto/

# Touch main.rs to invalidate the placeholder build
RUN touch gateway-core/src/main.rs

# Build the real binary
RUN cd gateway-core && cargo build --release

# ── Stage 2: Runtime ──
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

RUN useradd --system --create-home nexusgate

COPY --from=builder /build/gateway-core/target/release/nexusgate-gateway /usr/local/bin/nexusgate-gateway

# Default config location
RUN mkdir -p /etc/nexusgate
COPY deployments/nexusgate.toml /etc/nexusgate/nexusgate.toml

USER nexusgate

ENV RUST_LOG=nexusgate_gateway=info,tower_http=info
ENV NEXUSGATE_CONFIG=/etc/nexusgate/nexusgate.toml

EXPOSE 8080 8090 9090

ENTRYPOINT ["nexusgate-gateway"]
