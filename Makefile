.PHONY: help build test lint clean docker-up docker-down migrate seed

# Go
GO = go
GOFLAGS = -trimpath

# Services
SERVICES = api-gateway source-sync orchestrator worker writeback notification

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build all services
	$(GO) build $(GOFLAGS) ./...

build-service: ## Build a specific service (make build-service SERVICE=api-gateway)
	$(GO) build $(GOFLAGS) -o bin/$(SERVICE) ./cmd/$(SERVICE)

build-all-services: ## Build all service binaries to bin/
	@mkdir -p bin
	@for svc in $(SERVICES); do \
		echo "Building $$svc..."; \
		$(GO) build $(GOFLAGS) -o bin/$$svc ./cmd/$$svc; \
	done

test: ## Run all tests
	$(GO) test ./...

test-verbose: ## Run all tests with verbose output
	$(GO) test -v ./...

test-coverage: ## Run tests with coverage report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

lint: ## Run linters
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed" && exit 1)
	golangci-lint run ./...

vet: ## Run go vet
	$(GO) vet ./...

clean: ## Clean build artifacts
	rm -rf bin/ coverage.out coverage.html

# Docker
docker-up: ## Start all services with Docker Compose
	docker compose -f deploy/docker-compose/docker-compose.yml up -d

docker-down: ## Stop all services
	docker compose -f deploy/docker-compose/docker-compose.yml down

docker-logs: ## Show logs for all services
	docker compose -f deploy/docker-compose/docker-compose.yml logs -f

docker-build: ## Build all Docker images
	docker compose -f deploy/docker-compose/docker-compose.yml build

docker-infra: ## Start only infrastructure (postgres + nats)
	docker compose -f deploy/docker-compose/docker-compose.yml up -d postgres nats

# Database
migrate-up: ## Run all pending migrations
	@echo "Running migrations..."
	$(GO) run ./cmd/api-gateway migrate

migrate-down: ## Rollback last migration
	@echo "Rolling back last migration..."
	$(GO) run ./cmd/api-gateway migrate-down

db-shell: ## Open psql shell
	docker compose -f deploy/docker-compose/docker-compose.yml exec postgres psql -U runeforge runeforge

# Development
dev-api: ## Run api-gateway locally
	LOG_LEVEL=debug go run ./cmd/api-gateway

dev-sync: ## Run source-sync locally
	LOG_LEVEL=debug go run ./cmd/source-sync

dev-orchestrator: ## Run orchestrator locally
	LOG_LEVEL=debug go run ./cmd/orchestrator

dev-worker: ## Run worker locally
	LOG_LEVEL=debug PROMPTS_PATH=./prompts go run ./cmd/worker

dev-admin: ## Run admin web locally
	cd web/admin && npm run dev

# Code generation
generate: ## Run go generate
	$(GO) generate ./...

# Dependencies
deps: ## Download dependencies
	$(GO) mod download
	$(GO) mod tidy

# Seed
seed: ## Seed database with test data
	@echo "No seed script yet"

# Health checks
health: ## Check health of running services
	@curl -s http://localhost:8080/health | jq . || echo "api-gateway not responding"
	@curl -s http://localhost:8081/health | jq . || echo "source-sync not responding"
	@curl -s http://localhost:8082/health | jq . || echo "orchestrator not responding"
	@curl -s http://localhost:8083/health | jq . || echo "worker not responding"
