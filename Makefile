# Ledit Testing and Build Makefile
# Provides clear commands for different types of tests and builds

.PHONY: help test test-unit test-unit-lowmem test-integration test-e2e test-smoke test-desktop-smoke test-all test-ci test-coverage \
       clean build build-all install build-version build-ui deploy-ui build-wasm \
       verify-ui-embedded test-webui lint lint-fix dev build-webui-dist build-webui-dist-local \
       verify-dist verify-dist-local

# Default target
help:
	@echo "Ledit Testing and Build Commands:"
	@echo ""
	@echo "  make test-unit        - Run unit tests (fast, no dependencies)"
	@echo "  make test-unit-lowmem - Run unit tests in ~4GB RAM (no -race, low parallelism)"
	@echo "  make test-integration - Run integration tests (mocked AI)"  
	@echo "  make test-e2e         - Run e2e tests (requires AI model)"
	@echo "  make test-smoke       - Run smoke tests (basic functionality)"
	@echo "  make test-desktop-smoke - Run desktop Electron smoke tests"
	@echo "  make test-all         - Run unit + integration + smoke tests"
	@echo "  make test-coverage    - Run unit tests with coverage check (fails if < 40%)"
	@echo "  make clean            - Clean test artifacts"
	@echo ""
	@echo "Build Commands:"
	@echo "  make build            - Build sprout binary only"
	@echo "  make build-all        - Full build (UI + WASM + binary)"
	@echo "  make install          - Build and install to ~/.local/bin/sprout"
	@echo "  make build-fast       - Fast incremental build (skips unchanged UI)"
	@echo "  make build-version    - Build with version information"
	@echo "  make build-ui         - Build React web UI only"
	@echo "  make deploy-ui        - Build and deploy React UI (incremental)"
	@echo "  make build-wasm       - Build WASM shell module"
	@echo "  make verify-ui-embedded - Fail if embedded UI assets are stale"
	@echo "  make test-webui       - Test React web UI server"
	@echo "  make lint             - Lint frontend code"
	@echo "  make lint-fix         - Auto-fix frontend linting issues"
	@echo "Distribution Bundles:"
	@echo "  make build-webui-dist       - Build cloud-mode distributable WebUI bundle"
	@echo "  make build-webui-dist-local - Build local-mode distributable WebUI bundle"
	@echo "  make build-cloud            - Build cloud-mode binary (sprout-cloud)"
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

# Copy tree-sitter grammar blobs from the gotreesitter module cache into
# pkg/ast/grammars/bin/ (gitignored).  Required before any go build / go test
# because pkg/ast/grammars_embed.go references them via //go:embed.  See
# SP-058 for the design rationale.
.PHONY: prepare-grammars
prepare-grammars:
	@bash scripts/prepare-grammars.sh

# Test parallelism knobs. Peak test-suite memory is roughly
#   (TEST_P concurrent test binaries) x (per-binary working set, inflated by
#   the race detector's ~5-10x shadow memory). Measured per-package peaks with
#   -race: pkg/embedding 1.15GB, pkg/agent_tools 940MB, pkg/agent 910MB, cmd
#   and pkg/webui ~645MB. With internal -parallel multiplying inside each
#   binary, the Go defaults (-p / -parallel = GOMAXPROCS) peak this suite at
#   30-40GB. TEST_P=2 keeps -race runs inside ~10GB (fits a 16GB laptop);
#   TEST_P=4 needs 24GB+ headroom. Drop -race (see test-unit-lowmem) and the
#   whole suite fits ~4GB. Override on the CLI, e.g.
#   `make test-unit TEST_P=8 TEST_PARALLEL=8` or `make test-unit TEST_RACE=`.
TEST_RACE     ?= -race
TEST_P        ?= 2
TEST_PARALLEL ?= 4

# Unit Tests - Fast, no external dependencies
test-unit: prepare-grammars
	@echo "Running unit tests (race=$(TEST_RACE) -p $(TEST_P) -parallel $(TEST_PARALLEL))..."
	@bash -lc 'set -o pipefail; \
	go test $(TEST_RACE) -tags "browser grammar_blobs_external" ./pkg/... ./cmd/... -v -timeout=300s -short -p $(TEST_P) -parallel $(TEST_PARALLEL) -coverprofile=/tmp/sprout-unit-coverage.out 2>&1 | tee /tmp/sprout-test-unit.log; \
	status=$${PIPESTATUS[0]}; \
	if [ $$status -ne 0 ]; then \
		echo ""; \
		echo "Unit tests failed. Last 200 lines:"; \
		tail -n 200 /tmp/sprout-test-unit.log || true; \
		echo ""; \
		echo "Failing packages:"; \
		grep -nE "^(FAIL|--- FAIL:|panic:)" /tmp/sprout-test-unit.log || true; \
		exit $$status; \
	fi'

# Unit Tests, minimal memory footprint - drops the race detector (its ~5-10x
# shadow memory is the single biggest multiplier) and tightens parallelism so
# the entire suite completes inside ~4GB RAM. Verified to pass under a hard
# `systemd-run -p MemoryMax=4G -p MemorySwapMax=0` cap. Use on memory-constrained
# machines or when -race isn't needed; CI/`test-coverage` still run with -race.
test-unit-lowmem:
	@$(MAKE) test-unit TEST_RACE= TEST_P=4 TEST_PARALLEL=2

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
	rm -f /tmp/sprout-coverage.out /tmp/sprout-unit-coverage.out /tmp/sprout-coverage-func.txt
	rm -f /tmp/sprout-test-coverage.log /tmp/sprout-test-unit.log
	find . -name "*.test" -delete
	find . -name "test_failure_*.log" -delete

# Quick test for development (just unit tests)
test: test-unit

# CI-friendly test (unit + integration)
test-ci: test-unit test-integration
	@echo "CI tests completed"

# Coverage Check - Run tests with coverage and enforce minimum threshold
# Note: timeout is the per-test-binary cap, not the wall clock. -race slows
# pkg/agent + pkg/embedding enough that 10m wasn't enough; 20m gives headroom.
test-coverage: prepare-grammars
	@echo "Running unit tests with coverage check..."
	@bash -lc 'set -o pipefail; \
	go test -race -tags "browser grammar_blobs_external" ./pkg/... ./cmd/... -timeout=1200s -p $(TEST_P) -parallel $(TEST_PARALLEL) -coverprofile=/tmp/sprout-coverage.out 2>&1 | tee /tmp/sprout-test-coverage.log; \
	status=$${PIPESTATUS[0]}; \
	if [ $$status -ne 0 ]; then \
		echo ""; \
		echo "Tests failed with race detection enabled. Last 200 lines:"; \
		tail -n 200 /tmp/sprout-test-coverage.log || true; \
		exit $$status; \
	fi; \
	echo ""; \
	echo "Generating coverage report..."; \
	go tool cover -func=/tmp/sprout-coverage.out > /tmp/sprout-coverage-func.txt; \
	total_coverage=$$(go tool cover -func=/tmp/sprout-coverage.out | grep "^total:" | awk "{print \$$3}" | sed "s/%//"); \
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
	if awk "BEGIN {exit !($${total_coverage} < $${min_coverage})}"; then \
		echo ""; \
		echo "ERROR: Coverage ($${total_coverage}%) is below minimum threshold ($${min_coverage}%)"; \
		echo "Packages with lowest coverage:"; \
		go tool cover -func=/tmp/sprout-coverage.out | grep -v "^total:" | awk -F" " "{print \$$NF, \$$0}" | sort -n | head -10 | awk "{\$$1=\"\"; print substr(\$$0,2)}"; \
		exit 1; \
	fi; \
	echo ""; \
	echo "Coverage check passed: $${total_coverage}% >= $${min_coverage}%"'

# Build sprout binary
# Optimized: uses build cache and parallel compilation
build: prepare-grammars
	@echo "Building sprout..."
	GO111MODULE=on go build -tags grammar_blobs_external -o sprout .
	@echo "Build completed"

# Install sprout binary to all common locations
install: build
	@echo "Installing sprout..."
	@mkdir -p ~/.local/bin ~/go/bin
	cp sprout ~/.local/bin/sprout
	cp sprout ~/go/bin/sprout 2>/dev/null || true
	@echo "Install completed"

# Build sprout binary with parallel compilation and cache
build-parallel: prepare-grammars
	@echo "Building sprout (parallel)..."
	GO111MODULE=on GOFLAGS="-p=8" go build -tags grammar_blobs_external -o sprout .
	@echo "Build completed"

# Build with version information
build-version:
	@echo "Building sprout with version information..."
	./scripts/version-manager.sh build
	@echo "Versioned build completed"

# React Web UI Commands

# Check if React UI needs rebuild (incremental build support)
check-needs-react-rebuild:
	@bash scripts/check-needs-react-rebuild.sh

# Lint frontend code
lint:
	@echo "Linting frontend code..."
	@cd webui && npm run lint && npm run format:check && npm run type-check && echo "Lint completed successfully"

# Auto-fix frontend linting issues
lint-fix:
	@echo "Auto-fixing frontend linting issues..."
	@cd webui && npm run lint:fix && npm run format && echo "Lint fix completed"

# Build React web UI only (doesn't deploy to Go static)
# Root npm ci installs every workspace (packages/events, packages/ui, webui);
# the @sprout/* packages are then built explicitly because their `prepare`
# script was removed in 61ba3f17 and webui resolves their `exports` from
# `dist/` at Vite-bundle time.
build-ui:
	@echo "Building React web UI with Vite..."
	@if [ ! -d "webui" ]; then \
		echo "Error: webui directory not found"; \
		exit 1; \
	fi
	@if [ ! -d node_modules ]; then npm ci; fi
	@npm run build -w @sprout/events
	@npm run build -w @sprout/ui
	@npm run build -w sprout-webui
	@echo "React web UI build completed in webui/dist/"

# Build React web UI and deploy to Go static directory (for embedding)
# Optimized: skips React build if source files haven't changed
deploy-ui:
	@echo "Checking if React UI needs rebuild..."
	@if bash scripts/check-needs-react-rebuild.sh; then \
		echo "Building React web UI with Vite..."; \
		if [ ! -d node_modules ]; then npm ci; fi; \
		npm run build -w @sprout/events; \
		npm run build -w @sprout/ui; \
		npm run build -w sprout-webui; \
		echo "React web UI build completed in webui/dist/"; \
		node scripts/build-webui-embed.mjs --no-build; \
	else \
		echo "React UI is up-to-date, skipping rebuild"; \
		echo "Deploying existing React build to Go static directory..."; \
		cd "$(CURDIR)" && node scripts/build-webui-embed.mjs --no-build; \
	fi
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
build-wasm: prepare-grammars
	@echo "Building WASM shell module..."
	@./scripts/build-wasm.sh
	@echo "WASM shell module build completed"

# Build cloud-mode distributable WebUI bundle (sets VITE_SPROUT_MODE=cloud)
build-webui-dist:
	@echo "Building cloud-mode WebUI distribution..."
	@node scripts/build-webui-dist.mjs --mode cloud
	@echo "Cloud-mode distribution ready in dist/cloud/"

# Build local-mode distributable WebUI bundle (sets VITE_SPROUT_MODE=local)
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

# Export endpoint manifest (for Foundry Service Worker sync)
export-endpoint-manifest:
	@node scripts/export-endpoint-manifest.mjs

# Full development build: UI + WASM + Go binary
# Optimized: skips React rebuild if source files haven't changed
#
# Order matters: build-wasm refreshes webui/public/wasm/sprout.wasm
# which Vite copies verbatim into webui/dist/ on the deploy-ui step.
# Running deploy-ui BEFORE build-wasm would embed the previous WASM
# blob into pkg/webui/static and ship it inside the Go binary, leaving
# users on the prior turn's WASM until the next full build.
build-all: build-wasm deploy-ui build
	@echo "Full build completed: React UI + WASM shell + Go binary"

# Generate the shared Go→TS type contract at webui/src/types/generated.ts.
#
# SP-034-5a (the actual generator wiring) is deferred — until that
# lands, this target is a verification-only no-op. It looks for the
# `@ts-generated` marker comments on the canonical Go types and warns
# if any are missing or if the TS file is out of sync (lexical hint
# only; not a full schema check). When tygo or an equivalent generator
# is wired up, replace the body below with the actual emit command.
generate-ts-types:
	@echo "[gen-ts] Verifying @ts-generated markers on canonical Go types..."
	@grep -lr "@ts-generated" pkg/webui pkg/events 2>/dev/null | sed 's/^/[gen-ts]   marked: /' || echo "[gen-ts]   (no @ts-generated markers found yet)"
	@test -f webui/src/types/generated.ts || { echo "[gen-ts] webui/src/types/generated.ts missing — hand-author it from the marked Go types" >&2; exit 1; }
	@echo "[gen-ts] OK. SP-034-5a will replace this with a real generator run."
.PHONY: generate-ts-types

# Build cloud-mode binary (sprout-cloud) — embeds cloud-mode WebUI
# Produces a separate binary so it doesn't overwrite the local-mode 'sprout'
build-cloud: build-wasm
	@echo "Building cloud-mode WebUI..."
	@cd webui && npm run build:cloud || exit 1
	@cd "$(CURDIR)" && node scripts/build-webui-embed.mjs
	@echo "Building sprout-cloud..."
	GO111MODULE=on go build -o sprout-cloud .
	@echo "Cloud build completed: sprout-cloud"

# Fast incremental build (only builds what changed)
build-fast:
	@echo "🚀 Fast incremental build..."
	@# Skip React if unchanged, always rebuild WASM and Go binary
	@if bash scripts/check-needs-react-rebuild.sh; then \
		echo "  Building React UI with Vite..."; \
		cd webui && npm run build || exit 1; \
		cd "$(CURDIR)" && node scripts/build-webui-embed.mjs || exit 1; \
	else \
		echo "  React UI up-to-date (skipped)"; \
	fi
	@echo "  Building WASM..."
	@./scripts/build-wasm.sh
	@echo "  Building Go binary..."
	@go build -o sprout .
	@echo "✅ Fast build completed"

# Quick development workflow
dev: deploy-ui
	@echo "Development build ready: React UI deployed"
	@echo "Run 'make build' to create Go binary with embedded UI"
