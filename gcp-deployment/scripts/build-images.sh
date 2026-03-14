#!/usr/bin/env bash
# ============================================================================
# Build and push NexusGate container images to Artifact Registry.
# This script treats the nexusgate source repo as an external dependency —
# it clones (or uses a local copy) and builds from the Dockerfiles in this
# deploy repo.
#
# Usage:
#   ./scripts/build-images.sh                    # builds all services
#   ./scripts/build-images.sh gateway-core       # builds one service
#
# Required env vars:
#   GCP_PROJECT_ID    — GCP project ID
#   GCP_REGION        — GCP region (default: us-central1)
#   NEXUSGATE_SRC     — Path to nexusgate source repo (default: ../nexusgate)
#   IMAGE_TAG         — Image tag (default: latest)
# ============================================================================
set -euo pipefail

GCP_PROJECT_ID="${GCP_PROJECT_ID:?Set GCP_PROJECT_ID}"
GCP_REGION="${GCP_REGION:-us-central1}"
NEXUSGATE_SRC="${NEXUSGATE_SRC:-../nexusgate}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
REGISTRY="${GCP_REGION}-docker.pkg.dev/${GCP_PROJECT_ID}/nexusgate"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(dirname "$SCRIPT_DIR")"

SERVICES=(gateway-core ai-orchestrator contract-engine security)

# ── Validate source repo ──
if [ ! -d "$NEXUSGATE_SRC" ]; then
    echo "ERROR: NexusGate source repo not found at $NEXUSGATE_SRC"
    echo "Set NEXUSGATE_SRC to the path of your local nexusgate clone."
    exit 1
fi

NEXUSGATE_SRC="$(cd "$NEXUSGATE_SRC" && pwd)"
echo "Using NexusGate source from: $NEXUSGATE_SRC"

# ── Configure Docker for Artifact Registry ──
echo "Configuring Docker auth for Artifact Registry..."
gcloud auth configure-docker "${GCP_REGION}-docker.pkg.dev" --quiet

# ── Build function ──
build_service() {
    local service="$1"
    local dockerfile="${DEPLOY_DIR}/docker/${service}.Dockerfile"
    local image="${REGISTRY}/${service}:${IMAGE_TAG}"

    if [ ! -f "$dockerfile" ]; then
        echo "ERROR: Dockerfile not found: $dockerfile"
        return 1
    fi

    echo ""
    echo "━━━ Building ${service} ━━━"
    echo "  Dockerfile: ${dockerfile}"
    echo "  Context:    ${NEXUSGATE_SRC}"
    echo "  Image:      ${image}"
    echo ""

    docker build \
        -f "$dockerfile" \
        -t "$image" \
        "$NEXUSGATE_SRC"

    echo ""
    echo "━━━ Pushing ${service} ━━━"
    docker push "$image"

    echo "✓ ${service} → ${image}"
}

# ── Main ──
if [ $# -gt 0 ]; then
    # Build specific service(s)
    for svc in "$@"; do
        build_service "$svc"
    done
else
    # Build all services
    for svc in "${SERVICES[@]}"; do
        build_service "$svc"
    done
fi

echo ""
echo "All images built and pushed to ${REGISTRY}"
