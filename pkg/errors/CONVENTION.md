# Error Handling Convention

## Overview

This document defines the standardized error handling patterns used throughout the ledit codebase.

## Core Principles

1. **Wrap errors with context** - Always use `fmt.Errorf("context: %w", err)` when wrapping an underlying error
2. **Use appropriate error constructors** - Use `errors.New()` for validation errors and sentinel errors
3. **Avoid bare error returns** - Never return an error without adding context about what operation failed
4. **Leverage standard library patterns** - Use `errors.Is()` and `errors.As()` for error checking

## Patterns

### 1. Wrapping Errors with Context

**Recommended:**
```go
if err != nil {
    return fmt.Errorf("failed to read file: %w", err)
}
```

**Not Recommended:**
```go
if err != nil {
    return err  // Missing context
}
```

### 2. Validation Errors

Use `errors.New()` for validation errors where no underlying error exists:

**Recommended:**
```go
if input == "" {
    return errors.New("input cannot be empty")
}
```

### 3. Sentinel Errors

Define sentinel errors at package level for common error conditions:

**Recommended:**
```go
var (
    ErrOutsideWorkingDirectory = errors.New("file access outside working directory")
    ErrUINotAvailable         = errors.New("UI not available")
    ErrCancelled             = errors.New("user cancelled")
)
```

### 4. Error Checking

Use `errors.Is()` and `errors.As()` for error checking:

**Recommended:**
```go
if errors.Is(err, ErrUINotAvailable) {
    // Handle specific error
}

var pathErr *os.PathError
if errors.As(err, &pathErr) {
    // Handle specific error type
}
```

### 5. Utility Wrapper

The `utils.WrapError()` helper is available but not required:

```go
import "github.com/alantheprice/ledit/pkg/utils"

if err != nil {
    return utils.WrapError(err, "context description")
}
```

## Current State Analysis

After a comprehensive analysis of the codebase, the error handling patterns are already consistent and follow best practices:

- **Most error wrapping** uses `fmt.Errorf("context: %w", err)` correctly
- **Validation errors** use `errors.New()` appropriately
- **Sentinel errors** are defined at package level and used consistently
- **No bare error returns** found that should be wrapped with context

## Examples from Codebase

### Good Examples

**From `pkg/agent_commands/commands.go`:**
```go
if strings.HasPrefix(trimmed, "/") {
    prefix = "/"
} else if strings.HasPrefix(trimmed, "!") {
    prefix = "!"
} else {
    return errors.New("not a valid command (must start with / or !)")
}
```

**From `pkg/agent_commands/commit_types.go`:**
```go
if r.Status == "" {
    return errors.New("status field is required")
}
```

**From `pkg/agent/tool_handlers_shell.go`:**
```go
command, err := convertToString(args["command"], "command")
if err != nil {
    return "", fmt.Errorf("failed to convert command parameter: %w", err)
}
```

**From `pkg/filesystem/filesystem.go`:**
```go
var (
    ErrOutsideWorkingDirectory     = errors.New("file access outside working directory")
    ErrWriteOutsideWorkingDirectory = errors.New("file write outside working directory")
)
```

## Testing

Ensure error handling is tested:

```go
func TestOperation(t *testing.T) {
    err := Operation()
    if err == nil {
        t.Fatal("expected error")
    }
    if !errors.Is(err, ExpectedError) {
        t.Errorf("unexpected error: %v", err)
    }
}
```

## Maintenance

- Keep error messages clear and actionable
- Include enough context to diagnose issues from logs
- Update sentinel errors when adding new error conditions
- Test error paths thoroughly

## References

- [Go Error Handling Best Practices](https://go.dev/blog/error-handling-and-go)
- [Working with Errors in Go 1.13+](https://go.dev/blog/go1.13-errors)
- [errors package documentation](https://pkg.go.dev/errors)
