# Error Handling Convention for Ledit

## Overview

This document defines the standardized error handling convention for the Ledit project. The goal is to maintain consistency across all Go packages while following Go best practices.

## Core Principles

1. **Always wrap errors with context**: When returning an error from a lower layer, wrap it with context using `%w` to preserve the error chain
2. **Use sentinel errors sparingly**: Reserve `errors.New` for package-level sentinel errors that are checked with `errors.Is`
3. **Propagate cache misses directly**: Don't add extra context for expected failures (e.g., cache misses, file not found when optional)
4. **Use consistent error message format**: Start with lowercase, describe what failed and why

## Standard Patterns

### Pattern 1: Wrapping Errors (Most Common)

```go
// ✅ CORRECT: Wrap with context and preserve error chain
func LoadConfig() (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read config file: %w", err)
    }
    // ...
}

// ❌ INCORRECT: Return bare error without context
func LoadConfig() (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    // ...
}
```

### Pattern 2: Creating New Errors

```go
// ✅ CORRECT: Create new error with context
func ValidateInput(input string) error {
    if input == "" {
        return fmt.Errorf("input cannot be empty")
    }
    // ...
}

// ✅ CORRECT: Use errors.New for sentinel errors (package-level)
var ErrNotFound = errors.New("resource not found")

func GetResource(id string) (*Resource, error) {
    if resource == nil {
        return nil, ErrNotFound
    }
    // ...
}
```

### Pattern 3: Expected Failures (No Extra Context)

```go
// ✅ CORRECT: Propagate expected failures directly
func (c *Cache) Get(key string) (interface{}, error) {
    data, err := os.ReadFile(c.cachePath(key))
    if err != nil {
        if os.IsNotExist(err) {
            return nil, err // Cache miss - return original error
        }
        return nil, fmt.Errorf("failed to read cache file: %w", err)
    }
    // ...
}
```

### Pattern 4: Error Checking with Context

```go
// ✅ CORRECT: Check specific errors with context
func IsRetryable(err error) bool {
    if errors.Is(err, ErrTemporary) {
        return true
    }
    if errors.Is(err, os.ErrNotExist) {
        return false
    }
    // Check for transient network errors
    var netErr *net.OpError
    if errors.As(err, &netErr) {
        return netErr.Timeout()
    }
    return false
}
```

## Decision Tree

When handling an error, follow this decision tree:

```
Is this an expected failure (cache miss, optional file, etc.)?
├─ YES → Return the error directly (don't wrap)
└─ NO
    ├─ Is this a package-level sentinel error?
    │   ├─ YES → Use `errors.New("description")` or return existing sentinel
    │   └─ NO
    │       ├─ Does this error need to be checked with `errors.Is`?
    │       │   ├─ YES → Consider creating a sentinel error
    │       │   └─ NO → Use `fmt.Errorf("context: %w", err)`
    │
    Are we creating a new error (not wrapping)?
    ├─ YES → Use `fmt.Errorf("description")`
    └─ NO (wrapping existing error) → Use `fmt.Errorf("context: %w", err)`
```

## Error Message Format

1. **Start with lowercase** (unless it's a proper noun)
2. **Describe what failed**, not what went wrong
3. **Include relevant context** (function names, file paths, IDs)

```go
// ✅ Good
fmt.Errorf("failed to read config file: %w", err)
fmt.Errorf("failed to unmarshal JSON response from API: %w", err)
fmt.Errorf("failed to create directory %s: %w", dirPath, err)

// ❌ Avoid (too verbose)
fmt.Errorf("An error occurred while attempting to read the configuration file from disk: %w", err)

// ❌ Avoid (not descriptive)
fmt.Errorf("read error: %w", err)
```

## Package-Specific Guidelines

### pkg/agent
- Wrap errors from API calls with context about the operation being performed
- Use sentinel errors for expected conditions (e.g., `errProviderStartupClosed`)
- Include agent state in error context when relevant

### pkg/configuration
- Wrap filesystem and JSON parsing errors with clear context
- Distinguish between "not found" (optional) and "failed to read" (error)

### pkg/webcontent
- Wrap HTTP/network errors with context about the URL and operation
- Propagate cache misses directly without extra context

### pkg/webui
- Wrap WebSocket errors with context about the client and operation
- Include WebSocket connection details in error context when relevant

## Testing

When testing error handling:

```go
// ✅ Test error wrapping preserves chain
func TestLoadConfig_Error(t *testing.T) {
    // Force file read error
    err := LoadConfig("/nonexistent/path")
    if err == nil {
        t.Fatal("expected error, got nil")
    }

    // Verify error chain is preserved
    if !os.IsNotExist(err) {
        t.Errorf("expected file not found error, got: %v", err)
    }
}

// ✅ Test sentinel errors
func TestGetResource_NotFound(t *testing.T) {
    _, err := GetResource("nonexistent")
    if !errors.Is(err, ErrNotFound) {
        t.Errorf("expected ErrNotFound, got: %v", err)
    }
}
```

## Migration Checklist

When updating existing code:

- [ ] Replace bare `return err` with wrapped errors (unless it's an expected failure)
- [ ] Replace `fmt.Errorf("msg")` (without wrapping) with appropriate sentinel errors if needed for `errors.Is`
- [ ] Ensure all error messages start with lowercase
- [ ] Verify error messages describe what failed and why
- [ ] Add context to errors (function names, file paths, IDs)
- [ ] Run tests to ensure error chains are preserved
- [ ] Update tests that check error messages to be more flexible

## Related Documentation

- [Go Error Handling Best Practices](https://go.dev/blog/error-handling-and-go)
- [Working with Errors in Go 1.13+](https://go.dev/blog/go1.13-errors)
- Project's error utilities: `pkg/utils/errors.go`
