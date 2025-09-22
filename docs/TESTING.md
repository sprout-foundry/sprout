# Testing Strategy for Ledit

This document defines the testing strategy and guidelines for the ledit project.

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

### Integration Tests (`integration_tests/`)

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

**Command**: `make test-integration`

### E2E Tests (`e2e_tests/`)

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

**Command**: `make test-e2e MODEL=openai:gpt-4`

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
make test-unit test-integration
```

### Before Releasing
```bash
# Full validation (expensive)
make test-all
make test-e2e MODEL=your-preferred-model
```

### CI Pipeline
```bash
# Pull Request validation
make test-ci  # unit + integration

# Main branch validation  
make test-all # unit + integration + smoke
```

## 📁 File Organization

```
ledit/
├── Makefile                     # Test commands
├── TESTING.md                   # This file
├── pkg/                         # Unit tests (*_test.go)
├── cmd/                         # Command tests (*_test.go)
├── integration_tests/           # Integration tests
├── e2e_tests/                   # E2E tests
├── smoke_tests/                 # Smoke tests
├── integration_test_runner.py   # Integration test runner
└── e2e_test_runner.py          # E2E test runner
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

### Integration Test Guidelines

```bash
#!/bin/bash
# Good: Uses mock model, tests real CLI behavior
ledit agent "test command" --model test:test

# Bad: Uses real API in integration test
ledit agent "test command" --model openai:gpt-4  # EXPENSIVE
```

### E2E Test Guidelines

```bash
#!/bin/bash
# Good: Complete workflow with real model
ledit agent "Add error handling to main.go" --model $MODEL
# Verify the actual changes were made correctly

# Good: Validates real provider integration
ledit agent "Analyze this codebase" --provider openai --model gpt-4
```

## 🔧 Test Commands Reference

| Command | Purpose | Speed | Cost | Dependencies |
|---------|---------|-------|------|--------------|
| `make test-unit` | Unit tests | Very Fast | Free | None |
| `make test-integration` | Integration tests | Fast | Free | Mock AI |
| `make test-e2e MODEL=X` | E2E tests | Slow | $$$ | Real AI |
| `make test-smoke` | Smoke tests | Fast | Free | Optional |
| `make test-all` | Unit + Integration + Smoke | Fast | Free | Mock AI |
| `make test-ci` | CI-friendly tests | Fast | Free | Mock AI |

## 🚨 Troubleshooting

### Common Issues

**Unit tests failing with "unknown field"**:
- Tests reference old configuration structure
- Update test to use current Config fields

**Integration tests failing**:
- Check that `test:test` model is working
- Verify test scripts have execute permissions

**E2E tests expensive**:
- Use cheaper models when possible: `deepinfra:meta-llama/Llama-3.1-8B-Instruct`
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