# NexusGate

**AI-Native API Gateway & Contract Orchestration Platform**

NexusGate sits between your backend services and their consumers — human developers or AI agents — and uses generative AI to automatically discover service capabilities, generate typed contracts, and produce tailored SDKs in real time.

> Reduce API integration time from weeks to minutes by letting AI handle service discovery, contract generation, and SDK creation — while maintaining enterprise-grade security and governance.

## Architecture

```
┌──────────────────────────────────────────────────────┐
│              Consumer Interface Layer                  │
│   Portal (React)  │  Chat (NL)  │  Agent (SNP/MCP)   │
├──────────────────────────────────────────────────────┤
│              Security & Policy Layer                  │
│   OAuth2/OIDC  │  mTLS  │  RBAC  │  OPA Policies     │
├──────────────────────────────────────────────────────┤
│                 Contract Engine                        │
│   Schema Negotiation  │  SDK Gen  │  Contract Store    │
├──────────────────────────────────────────────────────┤
│              AI Orchestration Layer                    │
│   LLM Router  │  Capability Discovery  │  NL→Contract  │
├──────────────────────────────────────────────────────┤
│             Service Connector Layer                    │
│   REST  │  GraphQL  │  gRPC  │  Database  │  Queues   │
├──────────────────────────────────────────────────────┤
│              Gateway Core (Rust)                       │
│   HTTP Engine (Hyper/Tokio)  │  Router  │  Middleware  │
└──────────────────────────────────────────────────────┘
```

## Key Features

**AI-Powered Service Discovery** — Point NexusGate at any backend (REST API, GraphQL endpoint, gRPC service, or database) and the AI layer automatically introspects the schema, infers capabilities, and generates a normalized capability manifest.

**Dynamic Contract Generation** — Consumers describe what they need (in natural language or via protocol) and receive a typed, versioned contract with generated SDK code — no manual API spec writing required.

**Schema Negotiation Protocol (SNP)** — A machine-readable protocol for AI agents to discover capabilities, negotiate contracts, and consume services autonomously. MCP-compatible.

**Consumer-Shaped SDKs** — Every consumer gets an SDK tailored to their exact needs: language, shape, field subset, and access level. TypeScript, Python, Go, and OpenAPI output supported.

**Contract-Bound Security** — Every generated contract inherits authorization policies. OAuth2/OIDC, mTLS, RBAC, and OPA policy engine — all configurable per service, per consumer.

**Three Consumer Personas** — Chat interface for human developers, SNP/MCP protocol for AI agents, and a self-service developer portal for browsing and exploration.

## Tech Stack

| Component | Language | Key Dependencies |
|-----------|----------|-----------------|
| Gateway Core | Rust | Tokio, Hyper, Tower, DashMap |
| Service Connectors | Rust | reqwest, graphql-parser, prost, sqlx |
| AI Orchestrator | Go | Custom LLM client (Claude + OpenAI), chi |
| Contract Engine | Go | text/template, protobuf, openapi3 |
| Security Layer | Go | jwt-go, OPA, crypto/tls |
| Developer Portal | TypeScript | React, TanStack Query |
| Storage | — | PostgreSQL 16, Redis 7 |

## Repository Structure

```
nexusgate/
├── gateway-core/           # Rust: HTTP proxy, routing, middleware, connectors
│   ├── src/
│   │   ├── main.rs         # Server entrypoint
│   │   ├── server.rs       # Request handling & proxying
│   │   ├── routing.rs      # Dynamic route table (DashMap)
│   │   ├── proxy.rs        # Reverse proxy with retries
│   │   ├── middleware.rs    # Rate limiter, circuit breaker
│   │   ├── management.rs   # REST API for route registration
│   │   ├── config.rs       # TOML configuration
│   │   ├── error.rs        # Typed error handling
│   │   └── connectors/     # REST, GraphQL, gRPC, DB introspection
│   └── Cargo.toml
├── ai-orchestrator/        # Go: LLM integration & NL→Contract translation
│   ├── cmd/orchestrator/
│   └── internal/
│       ├── llm/            # Provider-agnostic LLM router (Claude, OpenAI)
│       ├── discovery/      # Capability manifest index & search
│       └── translator/     # Natural language → contract spec
├── contract-engine/        # Go: SDK generation & Schema Negotiation Protocol
│   ├── cmd/engine/
│   └── internal/
│       ├── generator/      # TypeScript, Python, OpenAPI generators
│       ├── snp/            # Schema Negotiation Protocol handler
│       └── store/          # Versioned contract storage
├── security/               # Go: Auth, RBAC, audit logging
│   ├── cmd/security/
│   └── internal/
│       ├── auth/           # JWT validation & token issuance
│       ├── rbac/           # Role-based access control engine
│       └── audit/          # Audit log
├── deployments/            # Configuration files
├── scripts/                # Database migrations
├── docker-compose.yml      # Full stack: gateway + Go services + Postgres + Redis
├── Makefile                # Build, test, run, lint commands
└── ARCHITECTURE.md         # Detailed technical architecture
```

## Quick Start

### Prerequisites

- Rust 1.75+ (for gateway-core)
- Go 1.22+ (for Go services)
- Docker & Docker Compose (for infrastructure)
- An LLM API key (Anthropic or OpenAI)

### Setup

```bash
# Clone
git clone https://github.com/geoffsdesk/nexusgate.git
cd nexusgate

# Configure
cp .env.example .env
# Edit .env — add your ANTHROPIC_API_KEY or OPENAI_API_KEY

# Start everything with Docker
make docker-up

# Or start infrastructure only and run services locally
make dev              # Starts Postgres + Redis
make run-gateway      # Terminal 1
make run-orchestrator # Terminal 2
make run-contract-engine # Terminal 3
make run-security     # Terminal 4
```

### Register a Service

```bash
# Register a REST service via the management API
curl -X POST http://localhost:8090/api/v1/routes \
  -H "Content-Type: application/json" \
  -d '{
    "path_pattern": "GET:/api/users/{id}",
    "methods": ["GET"],
    "service_name": "user-service",
    "upstream_url": "http://your-service:3000",
    "description": "User service"
  }'
```

### Generate a Contract (via AI)

```bash
# Translate natural language to a contract spec
curl -X POST http://localhost:8081/api/v1/translate \
  -H "Content-Type: application/json" \
  -d '{
    "description": "I need user CRUD with pagination and email search",
    "output_format": "typescript"
  }'
```

### Schema Negotiation (for AI Agents)

```bash
# Discover capabilities
curl http://localhost:8082/snp/capabilities

# Negotiate a contract
curl -X POST http://localhost:8082/snp/negotiate \
  -H "Content-Type: application/json" \
  -d '{
    "consumer_id": "agent-001",
    "needs": [
      {"operation": "read_users", "description": "Get user profiles"}
    ],
    "constraints": ["read-only"]
  }'

# Accept the proposal
curl -X POST http://localhost:8082/snp/accept/{proposalId}
```

## Service Ports

| Service | Port | Purpose |
|---------|------|---------|
| Gateway Core | 8080 | Public API gateway |
| Management API | 8090 | Route registration (internal) |
| Prometheus Metrics | 9090 | Gateway metrics |
| AI Orchestrator | 8081 | NL translation, capability discovery |
| Contract Engine | 8082 | SDK generation, SNP |
| Security Service | 8083 | Auth, RBAC, audit |
| PostgreSQL | 5432 | Primary data store |
| Redis | 6379 | Caching |

## MVP Roadmap

| Phase | Weeks | Deliverable |
|-------|-------|-------------|
| 1. Gateway Foundation | 1–4 | Working proxy for REST + database backends |
| 2. AI Layer | 5–8 | AI-generated capability manifests from service schemas |
| 3. Contract Engine | 9–12 | Typed SDK generation (TypeScript, Python) with live validation |
| 4. Security & Polish | 13–16 | Production-ready with OAuth2, RBAC, observability |

## Development

```bash
make build          # Build all components
make test           # Run all tests
make lint           # Lint Rust + Go
make clean          # Clean build artifacts
make docker-logs    # Tail all service logs
```

## License

Apache 2.0

---

Built with Rust, Go, and AI.
