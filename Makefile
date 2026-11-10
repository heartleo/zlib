.DEFAULT_GOAL := help
.PHONY: help build check fmt hooks test tidy vet

help: ## Show this help
	@awk -F ':.*## ' '/^[a-z]+:.*## / { printf "  %-6s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build ./zlib
	go build ./cmd/zlib

check: ## Run the pre-commit checks (mandatory; .githooks/pre-commit runs it)
	sh scripts/check.sh

fmt: ## Format Go code
	go fmt ./...

hooks: ## Enable the git hooks (once per clone)
	git config core.hooksPath .githooks

test: ## Run the tests
	go test ./...

tidy: ## Tidy go.mod and go.sum
	go mod tidy

vet: ## Run go vet
	go vet ./...
