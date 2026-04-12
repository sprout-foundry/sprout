# Ledit Testing and Build Makefile
# Provides clear commands for different types of tests and builds

.PHONY: help test test-unit test-integration test-e2e test-smoke test-all test-ci \
       clean build build-all build-version build-ui deploy-ui \
       verify-ui-embedded test-webui lint lint-fix dev

# Default target
help:
	@echo "Ledit Testing and Build Commands:"
	@echo ""
	@echo "  make test-unit        - Run unit tests (fast, no dependencies)"
	@echo "  make test-integration - Run integration tests (mocked AI)"  
	@echo "  make test-e2e         - Run e2e tests (requires AI model)"
	@echo "  make test-smoke       - Run smoke tests (basic functionality)"
	@echo "  make test-all         - Run unit + integration + smoke tests"
	@echo "  make clean            - Clean test artifacts"
	@echo "  make build            - Build ledit binary"
	@echo "  make build-version    - Build with version information"
	@echo "  make build-ui         - Build React web UI"
	@echo "  make deploy-ui        - Build and deploy React UI to Go static"
	@echo "  make verify-ui-embedded - Fail if embedded UI assets are stale"
	@echo "  make test-webui      - Test React web UI server"
	@echo "  make lint            - Lint frontend code"
	@echo "  make lint-fix        - Auto-fix frontend linting issues"
	@echo ""
	@echo "Version Management:"
	@echo "  ./scripts/version-manager.sh build    - Build with version info"
	@echo ""
	@echo "Examples:"
	@echo "  make test-unit                    # Quick feedback loop"
	@echo "  make test-e2e MODEL=openai:gpt-4  # Full e2e with real model"
	@echo "  make test-all                     # Pre-release validation"
	@echo "  make build-version                # Build with version info"
	@echo "  make deploy-ui                    # Build and deploy React UI"
	@echo "  make verify-ui-embedded          # Ensure embedded UI is current"
	@echo "  make test-webui                   # Test React web UI server"

# Unit Tests - Fast, no external dependencies
test-unit:
	@echo "Running unit tests..."
	@bash -lc 'set -o pipefail; \
	go test -tags ollama_test ./pkg/... ./cmd/... -v -timeout=60s -short 2>&1 | tee /tmp/ledit-test-unit.log; \
	status=$${PIPESTATUS[0]}; \
	if [ $$status -ne 0 ]; then \
		echo ""; \
		echo "Unit tests failed. Last 200 lines:"; \
		tail -n 200 /tmp/ledit-test-unit.log || true; \
		echo ""; \
		echo "Failing packages:"; \
		grep -nE "^(FAIL|--- FAIL:|panic:)" /tmp/ledit-test-unit.log || true; \
		exit $$status; \
	fi'

# Integration Tests - Mocked AI, file operations
test-integration:
	@echo "Running integration tests..."
	python3 integration_test_runner.py

# E2E Tests - Real LLM models (expensive)
test-e2e:
ifndef MODEL
	@echo "Error: MODEL is required for e2e tests"
	@echo "Example: make test-e2e MODEL=openai:gpt-4"
	@exit 1
endif
	@echo "Running e2e tests with model: $(MODEL)"
	@echo "This will use real API calls and cost money!"
	python3 e2e_test_runner.py -m $(MODEL)

# Smoke Tests - Basic functionality check
test-smoke:
	@echo "Running smoke tests..."
	cd smoke_tests && chmod +x run_api_test.sh && ./run_api_test.sh

# Test All (except expensive e2e)
test-all: test-unit test-integration test-smoke
	@echo "All tests completed (excluding e2e)"

# Clean test artifacts
clean:
	@echo "Cleaning test artifacts..."
	rm -rf testing/
	rm -f e2e_results.csv
	find . -name "*.test" -delete
	find . -name "test_failure_*.log" -delete

# Quick test for development (just unit tests)
test: test-unit

# CI-friendly test (unit + integration)
test-ci: test-unit test-integration
	@echo "CI tests completed"

# Build ledit binary
build:
	@echo "Building ledit..."
	go build -tags ollama_test -o ledit .
	@echo "Build completed"

# Build with version information
build-version:
	@echo "Building ledit with version information..."
	./scripts/version-manager.sh build
	@echo "Versioned build completed"

# React Web UI Commands

# Lint frontend code
lint:
	@echo "Linting frontend code..."
	@cd webui && npm run lint && npm run format:check && npm run type-check && echo "Lint completed successfully"

# Auto-fix frontend linting issues
lint-fix:
	@echo "Auto-fixing frontend linting issues..."
	@cd webui && npm run lint:fix && npm run format && echo "Lint fix completed"

# Build React web UI only (doesn't deploy to Go static)
build-ui:
	@echo "Building React web UI..."
	@if [ ! -d "webui" ]; then \
		echo "Error: webui directory not found"; \
		exit 1; \
	fi
	@# Install npm dependencies
	@cd webui && npm ci
	@cd webui && npm run build
	@echo "React web UI build completed in webui/build/"

# Build React web UI and deploy to Go static directory (for embedding)
deploy-ui: build-ui
	@echo "Deploying React web UI to Go static directory..."
	@if [ ! -d "webui" ]; then \
		echo "Error: webui directory not found"; \
		exit 1; \
	fi
	@node scripts/build-webui-embed.mjs
	@echo "React web UI deployed to pkg/webui/static/"
	@echo "Build artifacts in pkg/webui/static/ are now embedded at compile time."

verify-ui-embedded:
	@echo "Verifying webui/build/ assets are available..."
	@test -d webui/build || ( echo "webui/build/ does not exist. Run 'make build-ui'."; exit 1 )
	@test -f webui/build/index.html || ( echo "webui/build/index.html is missing. Run 'make build-ui'."; exit 1 )
	@echo "WebUI build assets are available (served from webui/build/ at runtime)"

# Test React web UI server
test-webui:
	@echo "Building and testing React web UI server..."
	@if [ ! -f "test/test_webserver" ]; then \
		echo "Building test web server..."; \
		go build -o test/test_webserver ./test/; \
	fi
	@echo "Starting React web UI test server on port 8801..."
	@echo "Open http://localhost:8801 to test the UI"
	@echo "Press Ctrl+C to stop the server"
	cd test && ./test_webserver

# Full development build: UI + Go binary
build-all: deploy-ui build
	@echo "Full build completed: React UI + Go binary"

# Quick development workflow
dev: deploy-ui
	@echo "Development build ready: React UI deployed"
	@echo "Run 'make build' to create Go binary with embedded UI"
