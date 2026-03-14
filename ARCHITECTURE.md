# NexusGate — Technical Architecture

## System Overview

NexusGate is a five-layer AI-native API gateway that dynamically generates typed contracts between backend services and consumers (human developers or AI agents).

```
┌─────────────────────────────────────────────────────────────┐
│                   Consumer Interface Layer                    │
│  ┌──────────┐  ┌──────────────────┐  ┌───────────────────┐  │
│  │  Portal   │  │  Chat Interface  │  │  Agent Protocol   │  │
│  │  (React)  │  │  (NL Config)     │  │  (SNP/MCP)        │  │
│  └──────────┘  └──────────────────┘  └───────────────────┘  │
├─────────────────────────────────────────────────────────────┤
│                   Security & Policy Layer                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐  │
│  │ OAuth2/  │  │  mTLS    │  │  RBAC    │  │  OPA       │  │
│  │ OIDC     │  │  Termn.  │  │  Engine  │  │  Policies  │  │
│  └──────────┘  └──────────┘  └──────────┘  └────────────┘  │
├─────────────────────────────────────────────────────────────┤
│                    Contract Engine                            │
│  ┌───────────────┐  ┌───────────────┐  ┌────────────────┐  │
│  │ Schema Negot.  │  │ SDK Generator │  │ Contract Store │  │
│  │ Protocol (SNP) │  │ (Multi-lang)  │  │ (Versioned)    │  │
│  └───────────────┘  └───────────────┘  └────────────────┘  │
├─────────────────────────────────────────────────────────────┤
│                  AI Orchestration Layer                       │
│  ┌───────────────┐  ┌───────────────┐  ┌────────────────┐  │
│  │ LLM Router    │  │ Capability    │  │ NL→Contract    │  │
│  │ (Multi-prov.) │  │ Discovery     │  │ Translator     │  │
│  └───────────────┘  └───────────────┘  └────────────────┘  │
├─────────────────────────────────────────────────────────────┤
│                Service Connector Layer                        │
│  ┌──────┐ ┌─────────┐ ┌──────┐ ┌────┐ ┌────────────────┐  │
│  │ REST │ │ GraphQL │ │ gRPC │ │ DB │ │ Message Queues │  │
│  └──────┘ └─────────┘ └──────┘ └────┘ └────────────────┘  │
├─────────────────────────────────────────────────────────────┤
│                    Gateway Core (Rust)                        │
│  ┌───────────────┐  ┌───────────────┐  ┌────────────────┐  │
│  │ HTTP Engine   │  │ Router        │  │ Middleware      │  │
│  │ (Hyper/Tokio) │  │ (Dynamic)     │  │ Pipeline        │  │
│  └───────────────┘  └───────────────┘  └────────────────┘  │
└─────────────────────────────────────────────────────────────┘
         ▼               ▼                ▼
   ┌──────────┐   ┌──────────┐    ┌──────────────┐
   │ Backend  │   │ Backend  │    │  Database    │
   │ Service  │   │ Service  │    │  (Postgres)  │
   └──────────┘   └──────────┘    └──────────────┘
```

## Layer Details

### 1. Gateway Core (Rust — `gateway-core/`)

The foundational layer handling all inbound traffic. Built on Tokio + Hyper for maximum throughput.

**Responsibilities:**
- HTTP/1.1, HTTP/2, WebSocket proxying
- Dynamic route registration (routes added/removed at runtime via internal API)
- Middleware pipeline (rate limiting, circuit breaking, request transformation)
- Health checks and readiness probes
- Metrics emission (Prometheus-compatible)
- TLS termination

**Key Design Decisions:**
- Async-first with Tokio runtime
- Zero-copy request forwarding where possible
- Route table uses a concurrent `DashMap` for lock-free reads
- Middleware is a trait-based pipeline (`Transform` trait)
- Internal gRPC API for route management from Go services

### 2. Service Connector Layer (Rust — `gateway-core/src/connectors/`)

Adapters that introspect and normalize backend service schemas.

**Connectors:**
- **REST**: OpenAPI/Swagger spec parsing, automatic endpoint discovery
- **GraphQL**: Schema introspection query, type mapping
- **gRPC**: Proto file parsing / reflection API
- **Database**: PostgreSQL `information_schema` introspection, query builder
- **Message Queues**: Schema registry integration (Kafka, RabbitMQ)

Each connector produces a **Capability Manifest** (JSON-LD):
```json
{
  "@context": "https://nexusgate.io/schema/v1",
  "@type": "ServiceCapability",
  "service": "user-service",
  "protocol": "rest",
  "capabilities": [
    {
      "operation": "getUser",
      "method": "GET",
      "path": "/users/{id}",
      "input": { "id": { "type": "string", "format": "uuid" } },
      "output": { "$ref": "#/schemas/User" }
    }
  ],
  "schemas": {
    "User": {
      "type": "object",
      "properties": {
        "id": { "type": "string" },
        "email": { "type": "string", "format": "email" },
        "name": { "type": "string" }
      }
    }
  }
}
```

### 3. AI Orchestration Layer (Go — `ai-orchestrator/`)

The intelligence layer that makes NexusGate "generative."

**Components:**
- **LLM Router**: Provider-agnostic interface (Claude, GPT, Llama). Structured output validation. Retry with fallback.
- **Capability Discovery**: Indexes all capability manifests. Semantic search over capabilities using embeddings.
- **NL→Contract Translator**: Converts natural language descriptions into contract specifications. "I need user data with email and creation date" → typed contract definition.

**LLM Integration Pattern:**
```
Consumer Request (NL) → Prompt Construction → LLM Call →
  Structured Output Validation → Contract Spec → SDK Generation
```

### 4. Contract Engine (Go — `contract-engine/`)

Generates, versions, and serves typed contracts/SDKs.

**Schema Negotiation Protocol (SNP):**
Machine-readable protocol for AI agents to discover and negotiate contracts.
```
Agent → DISCOVER /snp/capabilities → Capability Manifest List
Agent → NEGOTIATE /snp/contract → { needs: [...], constraints: [...] }
Gateway → PROPOSE → Contract Definition
Agent → ACCEPT/COUNTER → Final Contract
Gateway → GENERATE → SDK/Client Code
```

**SDK Generation:**
- TypeScript/JavaScript client
- Python client
- Go client
- OpenAPI spec output
- GraphQL schema output

### 5. Security & Policy Layer (Go — `security/`)

Contract-bound security: every generated contract inherits authorization policies.

- OAuth2/OIDC token validation (integrates with any IdP)
- mTLS for service-to-service
- RBAC with contract-level granularity
- OPA (Open Policy Agent) for custom policies
- Rate limiting per contract/consumer
- Audit logging

### 6. Consumer Interface Layer

- **Developer Portal** (React SPA): Browse capabilities, configure contracts via UI
- **Chat Interface**: Natural language contract configuration
- **Agent Protocol**: SNP + MCP compatibility for agentic frameworks

## Data Flow

### Human Developer Flow:
```
Developer → Portal/Chat → "I need user CRUD with pagination"
  → AI Orchestrator (NL→Contract) → Contract Engine (generate)
  → SDK delivered → Developer integrates → Requests flow through Gateway Core
```

### AI Agent Flow:
```
Agent → SNP DISCOVER → Capability list
Agent → SNP NEGOTIATE → Contract proposal
Agent → SNP ACCEPT → Contract finalized
Agent → Use contract → Requests flow through Gateway Core
```

## Inter-Service Communication

```
                    ┌─────────────┐
                    │ Gateway Core│ (Rust, port 8080)
                    │   gRPC API  │ (port 50051 — internal)
                    └──────┬──────┘
                           │ gRPC
              ┌────────────┼────────────┐
              ▼            ▼            ▼
     ┌────────────┐ ┌───────────┐ ┌──────────┐
     │AI Orchestr.│ │ Contract  │ │ Security │
     │ (Go:8081)  │ │ (Go:8082) │ │ (Go:8083)│
     └────────────┘ └───────────┘ └──────────┘
              │            │            │
              └────────────┼────────────┘
                           ▼
                    ┌─────────────┐
                    │  PostgreSQL │ (port 5432)
                    │  + Redis    │ (port 6379)
                    └─────────────┘
```

## Tech Stack Summary

| Component | Language | Key Dependencies |
|-----------|----------|-----------------|
| Gateway Core | Rust | tokio, hyper, tonic (gRPC), dashmap, tower |
| Service Connectors | Rust | reqwest, graphql-parser, prost, sqlx |
| AI Orchestrator | Go | langchaingo or custom LLM client, pgx |
| Contract Engine | Go | text/template, protobuf, openapi3 |
| Security Layer | Go | opa-go, jwt-go, crypto/tls |
| Portal | TypeScript | React, TanStack Query |
| Storage | — | PostgreSQL 16, Redis 7 |
