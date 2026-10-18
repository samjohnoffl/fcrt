# FCRT — developer entry points.
# Usage: make help

SHELL := /bin/bash
GO    ?= go

.PHONY: help build test test-go test-fe vet lint run worker up down logs migrate migrate-local clean compose-config

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

build: ## Build all Go binaries into ./bin
	$(GO) build -o bin/fcrt-api    ./cmd/api
	$(GO) build -o bin/fcrt-worker ./cmd/worker
	$(GO) build -o bin/migrate     ./cmd/migrate

test: test-go test-fe ## Run all Go and frontend tests

test-go: ## Run Go tests (short mode)
	$(GO) test -short ./...

test-fe: ## Run frontend tests
	cd frontend && npm test

vet: ## Run go vet across the module
	$(GO) vet ./...

run: ## Run the API locally (requires DATABASE_URL)
	$(GO) run ./cmd/api

worker: ## Run the worker locally (requires DATABASE_URL)
	$(GO) run ./cmd/worker

compose-config: ## Validate docker-compose.yml
	docker compose config --quiet && echo "compose config OK"

up: ## Start the local dev stack (postgres + migrate + api + worker)
	docker compose up --build -d
	@echo "API:      http://localhost:8080"
	@echo "Postgres: localhost:5432 (fcrt/fcrt)"

down: ## Stop the local dev stack
	docker compose down

logs: ## Tail logs of all services
	docker compose logs -f

migrate: ## Run pending migrations via the compose migrate service
	docker compose run --rm migrate

migrate-local: ## Run pending migrations locally (requires DATABASE_URL)
	$(GO) run ./cmd/migrate up

clean: ## Remove build artifacts
	rm -rf bin
