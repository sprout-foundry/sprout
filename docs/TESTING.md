# Testing Strategy for Sprout

This document defines the testing strategy and guidelines for the sprout project.

## 🎯 Testing Philosophy

We use a layered testing approach that balances speed, coverage, and cost:

1. **Unit Tests**: Fast feedback for core logic
2. **Integration Tests**: Component interaction without real AI costs  
3. **E2E Tests**: Full workflows with real AI models (expensive)
4. **Smoke Tests**: Basic functionality verification

## 📋 Test Categories

### Unit Tests (`go test ./...`)

**Purpose**: Test individual functions and components in isolation
**Dependencies**: None (no external services, files, or AI models)
**Speed**: Very fast (<30 seconds for entire suite)
**Cost**: Free
**When to run**: Every commit, pre-push hooks, CI

**Examples**:
- Configuration parsing
- Utility functions  
- Data structure operations
- Mock AI responses

**Command**: `make test-unit`

### Integration Tests (`pkg/` / `cmd/` unit suites)

**Purpose**: Test component interaction with mocked dependencies
**Dependencies**: File system, mock AI model (`test:test`)
**Speed**: Fast (<2 minutes)
**Cost**: Free
**When to run**: Before releases, after significant changes

**Examples**:
- CLI command execution
- File modification workflows
- Configuration loading
- Agent initialization

**Command**: `make test-unit` (integration-style tests are covered by the unit test suite)

### E2E Tests

**Purpose**: Test complete user workflows with real AI models
**Dependencies**: Real AI providers, API keys, internet
**Speed**: Slow (2-5 minutes per test)
**Cost**: $$$ (real API calls)
**When to run**: Manual testing, release validation

**Examples**:
- Complete code editing workflows
- Multi-step agent conversations
- Real provider integrations
- Full user journeys

**Command**: Manual testing with `sprout agent "..." --model <your-model>` against real providers

### Smoke Tests (`smoke_tests/`)

**Purpose**: Basic functionality and health checks
**Dependencies**: Optional API keys
**Speed**: Fast (<1 minute)
**Cost**: Free to minimal
**When to run**: After deployment, health checks

**Examples**:
- Binary builds correctly
- Basic API connectivity
- Core functionality works

**Command**: `make test-smoke`

## 🚀 Developer Workflow

### Daily Development
```bash
# Quick feedback during development
make test-unit

# Before committing changes
make test-unit
```

### Before Releasing
```bash
# Full validation
make test-all
```

### CI Pipeline
```bash
# Pull Request validation
make test-ci  # unit tests

# Main branch validation  
make test-all # unit + smoke
```

## 📁 File Organization

```
sprout/
├── Makefile                     # Test commands
├── TESTING.md                   # This file
├── pkg/                         # Unit tests (*_test.go)
├── cmd/                         # Command tests (*_test.go)
├── smoke_tests/                 # Smoke tests
└── test/                        # Playwright/desktop smoke tests (*.spec.js)
```

## 🛠 Writing Tests

### Unit Test Guidelines

```go
// Good: Fast, isolated, no dependencies
func TestConfigParsing(t *testing.T) {
    config := parseConfig(`{"model": "gpt-4"}`)
    assert.Equal(t, "gpt-4", config.Model)
}

// Bad: Requires external dependencies
func TestWithRealAPI(t *testing.T) {
    client := openai.NewClient("real-api-key")
    response := client.Complete("hello") // DON'T DO THIS
}
```

### E2E / Integration Testing

Integration and E2E test runners were removed (the test directories they
referenced no longer existed). For ad-hoc integration testing against a mock
model:

```bash
sprout agent "test command" --model test:test
```

## 🔧 Test Commands Reference

| Command | Purpose | Speed | Cost | Dependencies |
|---------|---------|-------|------|--------------|
| `make test-unit` | Unit tests | Very Fast | Free | None |
| `make test-smoke` | Smoke tests | Fast | Free | Optional |
| `make test-all` | Unit + Smoke | Fast | Free | None |
| `make test-ci` | CI-friendly tests | Fast | Free | None |

## 🚨 Troubleshooting

### Common Issues

**Unit tests failing with "unknown field"**:
- Tests reference old configuration structure
- Update test to use current Config fields

**Integration tests failing**:
- Check that `test:test` model is working
- Verify test scripts have execute permissions

**E2E tests expensive**:
- Use cheaper models when possible: `deepinfra:deepseek-ai/DeepSeek-V3.1-Terminus`
- Run selectively, not all tests every time

**Smoke tests failing**:
- Usually due to missing API keys (expected)
- Check basic functionality is working

### Getting Help

1. Check test output for specific error messages
2. Run individual test categories to isolate issues
3. Use `make clean` to remove test artifacts
4. Check that all required tools are installed (Go, Python 3)

## 📊 Test Metrics

We aim for:
- **Unit test coverage**: >80% of core logic
- **Integration test coverage**: All CLI commands and workflows
- **E2E test coverage**: Key user journeys
- **Smoke test coverage**: Basic functionality

Use `go test -coverprofile=coverage.out ./...` to check coverage.