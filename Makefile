# Ledit Testing and Build Makefile
# Provides clear commands for different types of tests and builds

.PHONY: help test-unit test-integration test-e2e test-smoke test-all clean build build-version build-ui deploy-ui test-webui

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
	@echo "  make test-webui      - Test React web UI server"
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
	@echo "  make test-webui                   # Test React web UI server"

# Unit Tests - Fast, no external dependencies
test-unit:
	@echo "Running unit tests..."
	go test ./pkg/... ./cmd/... -v -timeout=30s

# Integration Tests - Mocked AI, file operations
test-integration:
	@echo "Running integration tests..."
	python3 integration_test_runner.py

# E2E Tests - Real AI models (expensive)
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
	go build -o ledit .
	@echo "Build completed"

# Build with version information
build-version:
	@echo "Building ledit with version information..."
	./scripts/version-manager.sh build
	@echo "Versioned build completed"

# React Web UI Commands

# Build React web UI only (doesn't deploy to Go static)
build-ui:
	@echo "Building React web UI..."
	@if [ ! -d "webui" ]; then \
		echo "Error: webui directory not found"; \
		exit 1; \
	fi
	@# Install npm dependencies
	@cd webui && npm install
	@cd webui && npm run build
	@echo "React web UI build completed in webui/build/"

# Build React web UI and deploy to Go static directory
deploy-ui:
	@echo "Deploying React web UI to Go static directory..."
	@if [ ! -d "webui" ]; then \
		echo "Error: webui directory not found"; \
		exit 1; \
	fi
	@# Install npm dependencies
	@cd webui && npm install
	@# Build React UI
	@cd webui && npm run build
	@echo "React web UI build completed in webui/build/"
	@# Deploy to Go static directory
	@if [ ! -d "pkg/webui/static" ]; then \
		mkdir -p pkg/webui/static; \
	fi
	@rm -rf pkg/webui/static/*
	@# Copy root-level files (index.html, manifest.json, etc.)
	@cp webui/build/*.html webui/build/*.json webui/build/*.xml webui/build/sw.js pkg/webui/static/ 2>/dev/null || true
	@# Copy static assets directly (without the 'static/' prefix in the destination)
	@if [ -d "webui/build/static/js" ]; then \
		mkdir -p pkg/webui/static/js && \
		cp -r webui/build/static/js/* pkg/webui/static/js/; \
	fi
	@if [ -d "webui/build/static/css" ]; then \
		mkdir -p pkg/webui/static/css && \
		cp -r webui/build/static/css/* pkg/webui/static/css/; \
	fi
	@if [ -d "webui/build/static/media" ] && [ "$$(ls -A webui/build/static/media)" ]; then \
		mkdir -p pkg/webui/static/media && \
		cp -r webui/build/static/media/* pkg/webui/static/media/; \
	fi
	@echo "React web UI deployed to pkg/webui/static/"
	@echo "Run 'make build' to create Go binary with embedded UI"

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