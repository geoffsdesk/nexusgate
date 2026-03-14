.PHONY: all build test clean dev docker-up docker-down

all: build

# ── Build ──
build: build-gateway build-orchestrator build-contract-engine build-security

build-gateway:
	cd gateway-core && cargo build --release

build-orchestrator:
	cd ai-orchestrator && go build -o bin/orchestrator ./cmd/orchestrator

build-contract-engine:
	cd contract-engine && go build -o bin/engine ./cmd/engine

build-security:
	cd security && go build -o bin/security ./cmd/security

# ── Test ──
test: test-gateway test-orchestrator test-contract-engine test-security

test-gateway:
	cd gateway-core && cargo test

test-orchestrator:
	cd ai-orchestrator && go test ./...

test-contract-engine:
	cd contract-engine && go test ./...

test-security:
	cd security && go test ./...

# ── Development ──
dev:
	docker compose up -d postgres redis
	@echo "Infrastructure ready. Start services individually:"
	@echo "  make run-gateway"
	@echo "  make run-orchestrator"
	@echo "  make run-contract-engine"
	@echo "  make run-security"

run-gateway:
	cd gateway-core && RUST_LOG=debug cargo run

run-orchestrator:
	cd ai-orchestrator && go run ./cmd/orchestrator

run-contract-engine:
	cd contract-engine && go run ./cmd/engine

run-security:
	cd security && go run ./cmd/security

# ── Docker ──
docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# ── Clean ──
clean:
	cd gateway-core && cargo clean
	rm -f ai-orchestrator/bin/*
	rm -f contract-engine/bin/*
	rm -f security/bin/*

# ── Lint ──
lint: lint-gateway lint-go

lint-gateway:
	cd gateway-core && cargo clippy -- -D warnings

lint-go:
	cd ai-orchestrator && go vet ./...
	cd contract-engine && go vet ./...
	cd security && go vet ./...
