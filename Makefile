# Ledit Testing Makefile
# Provides clear commands for different types of tests

.PHONY: help test-unit test-integration test-e2e test-smoke test-all clean

# Default target
help:
	@echo "Ledit Testing Commands:"
	@echo ""
	@echo "  make test-unit        - Run unit tests (fast, no dependencies)"
	@echo "  make test-integration - Run integration tests (mocked AI)"  
	@echo "  make test-e2e         - Run e2e tests (requires AI model)"
	@echo "  make test-smoke       - Run smoke tests (basic functionality)"
	@echo "  make test-all         - Run unit + integration + smoke tests"
	@echo "  make clean            - Clean test artifacts"
	@echo ""
	@echo "Examples:"
	@echo "  make test-unit                    # Quick feedback loop"
	@echo "  make test-e2e MODEL=openai:gpt-4  # Full e2e with real model"
	@echo "  make test-all                     # Pre-release validation"

# Unit Tests - Fast, no external dependencies
test-unit:
	@echo "🧪 Running unit tests..."
	go test ./pkg/... ./cmd/... -v -timeout=30s

# Integration Tests - Mocked AI, file operations
test-integration:
	@echo "🔧 Running integration tests..."
	python3 integration_test_runner.py

# E2E Tests - Real AI models (expensive)
test-e2e:
ifndef MODEL
	@echo "❌ Error: MODEL is required for e2e tests"
	@echo "Example: make test-e2e MODEL=openai:gpt-4"
	@exit 1
endif
	@echo "🚀 Running e2e tests with model: $(MODEL)"
	@echo "⚠️  This will use real API calls and cost money!"
	python3 e2e_test_runner.py -m $(MODEL)

# Smoke Tests - Basic functionality check
test-smoke:
	@echo "💨 Running smoke tests..."
	cd smoke_tests && chmod +x run_api_test.sh && ./run_api_test.sh

# Test All (except expensive e2e)
test-all: test-unit test-integration test-smoke
	@echo "✅ All tests completed (excluding e2e)"

# Clean test artifacts
clean:
	@echo "🧹 Cleaning test artifacts..."
	rm -rf testing/
	rm -f e2e_results.csv
	find . -name "*.test" -delete
	find . -name "test_failure_*.log" -delete

# Quick test for development (just unit tests)
test: test-unit

# CI-friendly test (unit + integration)
test-ci: test-unit test-integration
	@echo "✅ CI tests completed"