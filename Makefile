.PHONY: ci test lint vet build run-api run-worker docker-up docker-down clean help

# Default target
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ---------- CI Pipeline (mirrors .github/workflows/ci.yml) ----------

ci: lint test build ## Run full CI pipeline locally (lint + test + build)
	@echo "\n✅ CI pipeline passed!"

lint: vet staticcheck ## Run all linters

vet: ## Run go vet
	go vet ./...

staticcheck: ## Run staticcheck
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...

test: ## Run tests with race detector
	go test ./... -v -race -count=1

test-cover: ## Run tests with coverage report
	go test ./... -race -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

build: ## Build API and Worker binaries
	CGO_ENABLED=0 go build -ldflags="-w -s" -o bin/api ./cmd/api
	CGO_ENABLED=0 go build -ldflags="-w -s" -o bin/worker ./cmd/worker
	@echo "Binaries: bin/api, bin/worker"

# ---------- Development ----------

run-api: ## Run API server locally
	go run cmd/api/main.go

run-worker: ## Run worker locally
	go run cmd/worker/main.go

docker-up: ## Start all services with Docker Compose
	docker compose up -d

docker-down: ## Stop all services
	docker compose down

docker-rebuild: ## Rebuild and restart API + Worker
	docker compose up -d --build api worker

tidy: ## Run go mod tidy
	go mod tidy

swagger: ## Generate Swagger docs
	@which swag > /dev/null 2>&1 || (echo "Installing swag..." && go install github.com/swaggo/swag/cmd/swag@latest)
	swag init -g cmd/api/main.go -o docs

clean: ## Remove build artifacts
	rm -rf bin/ coverage.out coverage.html
