.PHONY: lint test build clean fmt vet help

# Default target
.DEFAULT_GOAL := help

# Variables
GOLANGCI_LINT_TIMEOUT := 5m
TEST_FLAGS := -v -cover

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

lint: ## Run golangci-lint
	@echo "Running golangci-lint..."
	golangci-lint run --timeout=$(GOLANGCI_LINT_TIMEOUT)

test: ## Run tests with coverage
	@echo "Running tests..."
	go test $(TEST_FLAGS) ./...

build: ## Build the project
	@echo "Building..."
	go build ./...

fmt: ## Format code
	@echo "Formatting code..."
	go fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

clean: ## Clean build artifacts
	@echo "Cleaning..."
	go clean ./...

ci: lint test ## Run CI checks (lint + test)

all: fmt vet lint test build ## Run all checks and build
