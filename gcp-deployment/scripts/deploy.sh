#!/usr/bin/env bash
# ============================================================================
# Deploy NexusGate to GKE.
# Applies all Kubernetes manifests with proper templating.
#
# Usage:
#   ./scripts/deploy.sh
#
# Required env vars:
#   GCP_PROJECT_ID    — GCP project ID
#   GCP_REGION        — GCP region (default: us-central1)
#   IMAGE_TAG         — Image tag (default: latest)
#   NEXUSGATE_SRC     — Path to nexusgate source (for init-db.sql)
#
# Optional env vars:
#   CLOUD_SQL_IP      — Cloud SQL private IP (from terraform output)
# ============================================================================
set -euo pipefail

GCP_PROJECT_ID="${GCP_PROJECT_ID:?Set GCP_PROJECT_ID}"
GCP_REGION="${GCP_REGION:-us-central1}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
NEXUSGATE_SRC="${NEXUSGATE_SRC:-../nexusgate}"
CLOUD_SQL_IP="${CLOUD_SQL_IP:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(dirname "$SCRIPT_DIR")"
K8S_DIR="${DEPLOY_DIR}/k8s"

REGISTRY="${GCP_REGION}-docker.pkg.dev/${GCP_PROJECT_ID}/nexusgate"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  NexusGate GKE Deployment"
echo "  Project:  ${GCP_PROJECT_ID}"
echo "  Region:   ${GCP_REGION}"
echo "  Registry: ${REGISTRY}"
echo "  Tag:      ${IMAGE_TAG}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# ── Step 1: Get cluster credentials ──
echo "[1/6] Configuring kubectl..."
gcloud container clusters get-credentials nexusgate \
    --region "$GCP_REGION" \
    --project "$GCP_PROJECT_ID"

# ── Step 2: Create namespace ──
echo "[2/6] Creating namespace..."
kubectl apply -f "${K8S_DIR}/namespace.yaml"

# ── Step 3: Apply service account (with project ID substitution) ──
echo "[3/6] Applying service account..."
sed "s/PROJECT_ID/${GCP_PROJECT_ID}/g" "${K8S_DIR}/service-account.yaml" | kubectl apply -f -

# ── Step 4: Apply config and secrets ──
echo "[4/6] Applying ConfigMap..."
kubectl apply -f "${K8S_DIR}/configmap.yaml"

echo "  Checking for secrets..."
if ! kubectl get secret nexusgate-secrets -n nexusgate &>/dev/null; then
    echo ""
    echo "  WARNING: nexusgate-secrets not found."
    echo "  Create it with:"
    echo "    kubectl create secret generic nexusgate-secrets \\"
    echo "      --namespace nexusgate \\"
    echo "      --from-literal=database-url='postgres://nexusgate:PASSWORD@${CLOUD_SQL_IP:-CLOUD_SQL_IP}:5432/nexusgate?sslmode=disable' \\"
    echo "      --from-literal=anthropic-api-key='sk-ant-...' \\"
    echo "      --from-literal=openai-api-key='sk-...'"
    echo ""
    read -p "  Press Enter after creating the secret (or Ctrl+C to abort)..."
fi

# ── Step 5: Init database ──
echo "[5/6] Initializing database..."
if [ -f "${NEXUSGATE_SRC}/scripts/init-db.sql" ]; then
    # Create the SQL configmap from the source repo
    kubectl create configmap nexusgate-sql-init \
        --namespace nexusgate \
        --from-file=init-db.sql="${NEXUSGATE_SRC}/scripts/init-db.sql" \
        --dry-run=client -o yaml | kubectl apply -f -

    # Update Cloud SQL IP in the job manifest and apply
    if [ -n "$CLOUD_SQL_IP" ]; then
        sed "s/CLOUD_SQL_PRIVATE_IP/${CLOUD_SQL_IP}/g" "${K8S_DIR}/db-init-job.yaml" | kubectl apply -f -
    else
        echo "  WARNING: CLOUD_SQL_IP not set. Skipping db-init job."
        echo "  Run manually after setting CLOUD_SQL_IP."
    fi
else
    echo "  WARNING: init-db.sql not found at ${NEXUSGATE_SRC}/scripts/init-db.sql"
    echo "  Database must be initialized manually."
fi

# ── Step 6: Deploy services ──
echo "[6/6] Deploying services..."
for service in gateway-core ai-orchestrator contract-engine security; do
    echo "  Deploying ${service}..."
    # Replace REGISTRY_URL placeholder with actual registry
    for manifest in "${K8S_DIR}/${service}"/*.yaml; do
        sed "s|REGISTRY_URL|${REGISTRY}|g; s|:latest|:${IMAGE_TAG}|g" "$manifest" | kubectl apply -f -
    done
done

# ── Apply ingress ──
echo "  Applying ingress..."
kubectl apply -f "${K8S_DIR}/ingress.yaml"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Deployment complete!"
echo ""
echo "  Check status:"
echo "    kubectl get pods -n nexusgate"
echo "    kubectl get svc -n nexusgate"
echo "    kubectl get ingress -n nexusgate"
echo ""
echo "  View logs:"
echo "    kubectl logs -n nexusgate -l app.kubernetes.io/name=gateway-core -f"
echo ""
echo "  Get external IP (may take a few minutes):"
echo "    kubectl get ingress nexusgate-ingress -n nexusgate -o jsonpath='{.status.loadBalancer.ingress[0].ip}'"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
