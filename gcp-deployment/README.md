# NexusGate — GKE Deployment

Consumer deployment configuration for running [NexusGate](https://github.com/geoffradian6/nexusgate) on Google Kubernetes Engine with managed Cloud SQL and Memorystore.

Consumer/operator deployment configuration that treats the NexusGate source as an external dependency. All GCP infrastructure, container builds, and Kubernetes manifests live here without modifying the core project source.

## Architecture

```
                    ┌──────────────────────────────────┐
                    │         GCP Project               │
                    │                                    │
   Internet ──────▶│  ┌──────────┐    GKE Cluster       │
                    │  │ Ingress  │    (nexusgate ns)    │
                    │  │ (GCE LB) │                      │
                    │  └────┬─────┘                      │
                    │       │                            │
                    │  ┌────▼──────────────────────┐     │
                    │  │  gateway-core (Rust)  ×2  │     │
                    │  │  :8080 public  :8090 mgmt │     │
                    │  └────┬──────────────────────┘     │
                    │       │                            │
                    │  ┌────▼────┐ ┌───────────┐ ┌────┐ │
                    │  │ ai-orch │ │ contract  │ │sec │ │
                    │  │  :8081  │ │  :8082    │ │8083│ │
                    │  └────┬────┘ └─────┬─────┘ └──┬─┘ │
                    │       │            │          │    │
                    │  ┌────▼────────────▼──────────▼──┐ │
                    │  │    Cloud SQL (PostgreSQL 16)   │ │
                    │  │    Memorystore (Redis 7)       │ │
                    │  └───────────────────────────────┘ │
                    └──────────────────────────────────┘
```

## Prerequisites

- **gcloud CLI** — authenticated with a project that has billing enabled
- **Terraform** >= 1.5
- **Docker** — for building container images
- **kubectl** — for K8s management
- **NexusGate source** — the parent directory (set `NEXUSGATE_SRC=..` or it defaults to `../nexusgate`)

## Quickstart

### 1. Provision GCP Infrastructure

```bash
cp terraform/terraform.tfvars.example terraform/terraform.tfvars
# Edit terraform.tfvars with your project ID and database password

make infra-init
make infra-apply
```

This creates: VPC, GKE cluster (2 nodes, autoscaling to 5), Cloud SQL PostgreSQL 16, Memorystore Redis 7, Artifact Registry, Workload Identity bindings.

### 2. Build and Push Images

```bash
cp .env.example .env
# Set GCP_PROJECT_ID in .env

make build
```

Builds all four services from the NexusGate source using the Dockerfiles in `docker/` and pushes to Artifact Registry.

### 3. Configure Secrets

Get the Cloud SQL private IP from Terraform:

```bash
make infra-output
# Note the cloud_sql_private_ip value
```

Create Kubernetes secrets:

```bash
make secrets
# You'll be prompted for: database URL, Anthropic API key, OpenAI API key
```

Or manually:

```bash
kubectl create secret generic nexusgate-secrets \
  --namespace nexusgate \
  --from-literal=database-url='postgres://nexusgate:YOUR_PASS@CLOUD_SQL_IP:5432/nexusgate?sslmode=disable' \
  --from-literal=anthropic-api-key='sk-ant-...' \
  --from-literal=openai-api-key='sk-...'
```

### 4. Deploy

```bash
export CLOUD_SQL_IP=$(cd terraform && terraform output -raw cloud_sql_private_ip)
make deploy
```

### 5. Verify

```bash
make status
```

Get the external IP (takes 2-3 minutes after deploy):

```bash
kubectl get ingress nexusgate-ingress -n nexusgate
```

Test the gateway:

```bash
curl http://EXTERNAL_IP/health
```

## Repository Structure

```
gcp-deployment/
├── terraform/              # GCP infrastructure (GKE, Cloud SQL, Redis, IAM)
│   ├── main.tf
│   ├── variables.tf
│   ├── outputs.tf
│   └── terraform.tfvars.example
├── docker/                 # Dockerfiles for each NexusGate service
│   ├── gateway-core.Dockerfile
│   ├── ai-orchestrator.Dockerfile
│   ├── contract-engine.Dockerfile
│   └── security.Dockerfile
├── k8s/                    # Kubernetes manifests
│   ├── namespace.yaml
│   ├── service-account.yaml
│   ├── configmap.yaml
│   ├── secrets.yaml.example
│   ├── db-init-job.yaml
│   ├── ingress.yaml
│   ├── gateway-core/
│   ├── ai-orchestrator/
│   ├── contract-engine/
│   └── security/
├── scripts/                # Automation scripts
│   ├── build-images.sh
│   ├── deploy.sh
│   └── teardown.sh
├── config/                 # Gateway config overrides
│   └── nexusgate.toml
├── Makefile               # Orchestrates everything
└── .env.example
```

## Service Ports

| Service          | Internal Port | Exposed Via        |
|:-----------------|:--------------|:-------------------|
| gateway-core     | 8080          | Ingress (public)   |
| gateway-mgmt     | 8090          | Internal LB only   |
| gateway-metrics  | 9090          | ClusterIP          |
| ai-orchestrator  | 8081          | ClusterIP          |
| contract-engine  | 8082          | ClusterIP + Ingress (/snp) |
| security         | 8083          | ClusterIP          |

## Operations

```bash
make status          # Pod/service/ingress status
make logs            # Tail all NexusGate logs
make logs-gateway    # Tail gateway logs only
make restart         # Rolling restart all services
make teardown        # Remove K8s resources (keeps infra)
make infra-destroy   # Destroy all GCP resources
```

## Cost Estimate

Approximate monthly cost for the default configuration (us-central1):

| Resource               | Spec                     | ~Monthly  |
|:-----------------------|:-------------------------|:----------|
| GKE cluster            | 2× e2-standard-4 nodes  | $140      |
| Cloud SQL PostgreSQL   | db-custom-2-4096         | $75       |
| Memorystore Redis      | 1 GB Basic              | $35       |
| Load Balancer          | GCE L7                  | $20       |
| Artifact Registry      | Storage + egress         | $5        |
| **Total**              |                          | **~$275** |

Scale down to `e2-standard-2` nodes and `db-f1-micro` for dev/testing (~$80/mo).
