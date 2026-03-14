#!/usr/bin/env bash
# ============================================================================
# Tear down the NexusGate GKE deployment (K8s resources only).
# Does NOT destroy Terraform infrastructure.
# ============================================================================
set -euo pipefail

echo "This will delete all NexusGate Kubernetes resources."
read -p "Continue? [y/N] " confirm
if [[ "$confirm" != [yY] ]]; then
    echo "Aborted."
    exit 0
fi

echo "Deleting NexusGate namespace (and all resources within it)..."
kubectl delete namespace nexusgate --ignore-not-found

echo ""
echo "K8s resources removed."
echo "To destroy GCP infrastructure, run: cd terraform && terraform destroy"
