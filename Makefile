# Ledit Testing and Build Makefile
# Provides clear commands for different types of tests and builds

.PHONY: help test test-unit test-integration test-e2e test-smoke test-desktop-smoke test-all test-ci test-coverage \
       clean build build-all build-version build-ui deploy-ui build-wasm \
       verify-ui-embedded test-webui lint lint-fix dev build-webui-dist build-webui-dist-local \
       verify-dist verify-dist-local

# Default target
help:
	@echo "Ledit Testing and Build Commands:"
	@echo ""
	@echo "  make test-unit        - Run unit tests (fast, no dependencies)"
	@echo "  make test-integration - Run integration tests (mocked AI)"  
	@echo "  make test-e2e         - Run e2e tests (requires AI model)"
	@echo "  make test-smoke       - Run smoke tests (basic functionality)"
	@echo "  make test-desktop-smoke - Run desktop Electron smoke tests"
	@echo "  make test-all         - Run unit + integration + smoke tests"
	@echo "  make test-coverage    - Run unit tests with coverage check (fails if < 40%)"
	@echo "  make clean            - Clean test artifacts"
	@echo "  make build            - Build sprout binary"
	@echo "  make build-version    - Build with version information"
	@echo "  make build-ui         - Build React web UI"
	@echo "  make build-wasm      - Build WASM shell module (sprout.wasm)"
	@echo "  make deploy-ui        - Build and deploy React UI to Go static"
	@echo "  make verify-ui-embedded - Fail if embedded UI assets are stale"
	@echo "  make test-webui      - Test React web UI server"
	@echo "  make lint            - Lint frontend code"
	@echo "  make lint-fix        - Auto-fix frontend linting issues"
	@echo ""
	@echo "Distribution Bundles:"
	@echo "  make build-webui-dist       - Build cloud-mode distributable WebUI bundle"
	@echo "  make build-webui-dist-local - Build local-mode distributable WebUI bundle"
	@echo "  make verify-dist            - Verify cloud-mode dist bundle serves correctly"
	@echo "  make verify-dist-local      - Verify local-mode dist bundle serves correctly"
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
	go test -tags ollama_test ./pkg/... ./cmd/... -v -timeout=60s -short -coverprofile=/tmp/ledit-unit-coverage.out 2>&1 | tee /tmp/ledit-test-unit.log; \
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

# Desktop Electron Smoke Tests - Run with Playwright under xvfb
test-desktop-smoke:
	@echo "Running desktop Electron smoke tests..."
	@which xvfb-run >/dev/null 2>&1 || ( echo "Error: xvfb-run not found. Install xvfb (e.g., sudo apt-get install xvfb)."; exit 1 )
	xvfb-run --auto-servernum --server-args="-screen 0 1280x720x24" npx playwright test --config=playwright.config.js

# Test All (except expensive e2e)
test-all: test-unit test-integration test-smoke
	@echo "All tests completed (excluding e2e)"

# Clean test artifacts
clean:
	@echo "Cleaning test artifacts..."
	rm -rf testing/
	rm -f e2e_results.csv
	rm -f /tmp/ledit-coverage.out /tmp/ledit-unit-coverage.out /tmp/ledit-coverage-func.txt
	rm -f /tmp/ledit-test-coverage.log /tmp/ledit-test-unit.log
	find . -name "*.test" -delete
	find . -name "test_failure_*.log" -delete

# Quick test for development (just unit tests)
test: test-unit

# CI-friendly test (unit + integration)
test-ci: test-unit test-integration
	@echo "CI tests completed"

# Coverage Check - Run tests with coverage and enforce minimum threshold
# Note: timeout is 120s (vs 60s for test-unit) because race detection slows tests significantly
test-coverage:
	@echo "Running unit tests with coverage check..."
	@bash -lc 'set -o pipefail; \
	go test -race -tags ollama_test ./pkg/... ./cmd/... -timeout=120s -short -coverprofile=/tmp/ledit-coverage.out 2>&1 | tee /tmp/ledit-test-coverage.log; \
	status=$${PIPESTATUS[0]}; \
	if [ $$status -ne 0 ]; then \
		echo ""; \
		echo "Tests failed with race detection enabled. Last 200 lines:"; \
		tail -n 200 /tmp/ledit-test-coverage.log || true; \
		exit $$status; \
	fi; \
	echo ""; \
	echo "Generating coverage report..."; \
	go tool cover -func=/tmp/ledit-coverage.out > /tmp/ledit-coverage-func.txt; \
	total_coverage=$$(go tool cover -func=/tmp/ledit-coverage.out | grep "^total:" | awk "{print \$$3}" | sed "s/%//"); \
	if [ -z "$${total_coverage}" ]; then \
		echo "ERROR: Failed to extract coverage information"; \
		exit 1; \
	fi; \
	if ! echo "$${total_coverage}" | grep -qE "^[0-9]+\.?[0-9]*$$"; then \
		echo "ERROR: Invalid coverage value: $${total_coverage}"; \
		exit 1; \
	fi; \
	echo ""; \
	echo "Total coverage: $${total_coverage}%"; \
	min_coverage=40; \
	if ! awk "BEGIN {exit !($${total_coverage} < $${min_coverage})}"; then \
		echo ""; \
		echo "ERROR: Coverage ($${total_coverage}%) is below minimum threshold ($${min_coverage}%)"; \
		echo "Packages with lowest coverage:"; \
		go tool cover -func=/tmp/ledit-coverage.out | grep -v "^total:" | awk -F" " "{print \$$NF, \$$0}" | sort -n | head -10 | awk "{\$$1=\"\"; print substr(\$$0,2)}"; \
		exit 1; \
	fi; \
	echo ""; \
	echo "Coverage check passed: $${total_coverage}% >= $${min_coverage}%"'

# Build sprout binary
build:
	@echo "Building ledit..."
	go build -tags ollama_test -o sprout .
	@echo "Build completed"

# Build with version information
build-version:
	@echo "Building sprout with version information..."
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
	@cd webui && DISABLE_ESLINT_PLUGIN=true npm run build
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
	@echo "Verifying embedded UI assets are available..."
	@test -d pkg/webui/static || ( echo "pkg/webui/static/ does not exist. Run 'make deploy-ui'."; exit 1 )
	@test -f pkg/webui/static/index.html || ( echo "pkg/webui/static/index.html is missing. Run 'make deploy-ui'."; exit 1 )
	@echo "Embedded UI assets are available (served from pkg/webui/static/ in production)"

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

# Build WASM shell module (sprout.wasm + wasm_exec.js)
build-wasm:
	@echo "Building WASM shell module..."
	@./scripts/build-wasm.sh
	@echo "WASM shell module build completed"

# Build cloud-mode distributable WebUI bundle (sets REACT_APP_SPROUT_MODE=cloud)
build-webui-dist:
	@echo "Building cloud-mode WebUI distribution..."
	@node scripts/build-webui-dist.mjs --mode cloud
	@echo "Cloud-mode distribution ready in dist/cloud/"

# Build local-mode distributable WebUI bundle (omits REACT_APP_SPROUT_MODE)
build-webui-dist-local:
	@echo "Building local-mode WebUI distribution..."
	@node scripts/build-webui-dist.mjs --mode local
	@echo "Local-mode distribution ready in dist/local/"

# Verify cloud-mode dist bundle can be served from static HTTP server
verify-dist:
	@echo "Verifying cloud-mode dist bundle..."
	@bash scripts/verify-dist-bundle.sh dist/cloud

# Verify local-mode dist bundle can be served from static HTTP server
verify-dist-local:
	@echo "Verifying local-mode dist bundle..."
	@bash scripts/verify-dist-bundle.sh dist/local

# Full development build: UI + WASM + Go binary
build-all: deploy-ui build-wasm build
	@echo "Full build completed: React UI + WASM shell + Go binary"

# Quick development workflow
dev: deploy-ui
	@echo "Development build ready: React UI deployed"
	@echo "Run 'make build' to create Go binary with embedded UI"
