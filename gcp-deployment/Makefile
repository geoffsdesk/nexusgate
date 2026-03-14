# ============================================================================
# NexusGate GKE Deployment — Consumer Makefile
# Manages the full lifecycle: infra → build → deploy → teardown
# ============================================================================

# ── Configuration (override via env or .env file) ──
GCP_PROJECT_ID  ?= $(shell echo $$GCP_PROJECT_ID)
GCP_REGION      ?= us-central1
NEXUSGATE_SRC   ?= ../nexusgate
IMAGE_TAG       ?= latest

export GCP_PROJECT_ID GCP_REGION NEXUSGATE_SRC IMAGE_TAG

.PHONY: all infra-init infra-plan infra-apply infra-destroy \
        build build-gateway build-orchestrator build-contract build-security \
        deploy status logs teardown clean help

# ── Default target ──
all: help

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Infrastructure (Terraform)
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

infra-init:
	cd terraform && terraform init

infra-plan:
	cd terraform && terraform plan

infra-apply:
	cd terraform && terraform apply

infra-output:
	cd terraform && terraform output

infra-destroy:
	cd terraform && terraform destroy

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Container Images
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

build: ## Build and push all container images
	./scripts/build-images.sh

build-gateway: ## Build gateway-core image only
	./scripts/build-images.sh gateway-core

build-orchestrator: ## Build ai-orchestrator image only
	./scripts/build-images.sh ai-orchestrator

build-contract: ## Build contract-engine image only
	./scripts/build-images.sh contract-engine

build-security: ## Build security image only
	./scripts/build-images.sh security

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Kubernetes Deployment
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

deploy: ## Deploy NexusGate to GKE
	./scripts/deploy.sh

status: ## Show deployment status
	@echo "━━━ Pods ━━━"
	kubectl get pods -n nexusgate -o wide
	@echo ""
	@echo "━━━ Services ━━━"
	kubectl get svc -n nexusgate
	@echo ""
	@echo "━━━ Ingress ━━━"
	kubectl get ingress -n nexusgate

logs: ## Tail logs for all NexusGate pods
	kubectl logs -n nexusgate -l app.kubernetes.io/part-of=nexusgate -f --max-log-requests=10

logs-gateway: ## Tail gateway-core logs
	kubectl logs -n nexusgate -l app.kubernetes.io/name=gateway-core -f

logs-orchestrator: ## Tail ai-orchestrator logs
	kubectl logs -n nexusgate -l app.kubernetes.io/name=ai-orchestrator -f

teardown: ## Remove K8s deployment (keeps infrastructure)
	./scripts/teardown.sh

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Utilities
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

port-forward-gateway: ## Port-forward gateway to localhost:8080
	kubectl port-forward -n nexusgate svc/gateway-core 8080:8080

port-forward-management: ## Port-forward management API to localhost:8090
	kubectl port-forward -n nexusgate svc/gateway-core 8090:8090

secrets: ## Create secrets interactively
	@echo "Creating NexusGate secrets..."
	@read -p "Database URL: " db_url; \
	read -p "Anthropic API Key: " anthropic_key; \
	read -p "OpenAI API Key: " openai_key; \
	kubectl create secret generic nexusgate-secrets \
		--namespace nexusgate \
		--from-literal=database-url="$$db_url" \
		--from-literal=anthropic-api-key="$$anthropic_key" \
		--from-literal=openai-api-key="$$openai_key" \
		--dry-run=client -o yaml | kubectl apply -f -

restart: ## Rolling restart of all deployments
	kubectl rollout restart deployment -n nexusgate

clean: ## Remove local docker images
	docker rmi $(shell docker images --filter=reference='*/nexusgate/*' -q) 2>/dev/null || true

# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Help
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

help: ## Show this help
	@echo "NexusGate GKE Deployment"
	@echo ""
	@echo "Quickstart:"
	@echo "  1. make infra-init && make infra-apply   # provision GCP resources"
	@echo "  2. make build                             # build container images"
	@echo "  3. make secrets                           # configure API keys"
	@echo "  4. make deploy                            # deploy to GKE"
	@echo "  5. make status                            # check health"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'
