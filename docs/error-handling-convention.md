# Error Handling Convention

**Last Updated:** 2026-04-05

This document defines the project-wide error handling convention for Go code in ledit.

## Core Rules

### 1. Always Wrap Errors with Context

Use `fmt.Errorf` with `%w` verb to wrap errors, preserving the error chain for debugging and `errors.Is()`/`errors.As()` usage.

**Pattern:**
```go
return fmt.Errorf("operation: %w", err)
```

**Examples:**
```go
// ✅ Correct
return fmt.Errorf("failed to read file %s: %w", path, err)
return "", fmt.Errorf("git operation %s failed: %w", operation, err)

// ❌ Incorrect - no wrapping
return fmt.Errorf("failed to read file %s", path)

// ❌ Incorrect - bare error return
return err
```

### 2. Use StructuredError for Domain-Specific Errors

For errors that need severity, category, and context tracking, use the `StructuredError` builder from `pkg/utils/errors.go`.

**Pattern:**
```go
return utils.New<ErrorType>Error("operation", "resource", err).WithContext(context)
```

**Examples:**
```go
// ✅ Correct - using StructuredError
return utils.NewValidationError("parse", "config.json", err).WithComponent("configuration")
return utils.NewFileSystemError("read", path, err).WithComponent("filesystem")

// ❌ Incorrect - not using StructuredError for validation errors
return fmt.Errorf("invalid config: %w", err)
```

### 3. Use errors.New for Sentinel Errors

Define package-level sentinel errors with `errors.New` for use with `errors.Is()`.

**Pattern:**
```go
var Err<Name> = errors.New("<description>")
```

**Examples:**
```go
// ✅ Correct
var ErrNotFound = errors.New("resource not found")
if errors.Is(err, ErrNotFound) {
    // handle not found
}

// ❌ Incorrect - using fmt.Errorf for sentinel
var ErrNotFound = fmt.Errorf("resource not found")
```

### 4. Error Message Format

Error messages should be:
- **Lowercase** (no capital first letter)
- **Descriptive** (specific operation name)
- **No period at end**
- **Consistent phrasing**

**Examples:**
```go
// ✅ Correct
return fmt.Errorf("failed to create directory: %w", err)
return fmt.Errorf("invalid configuration: %w", err)
return fmt.Errorf("no API key provided")

// ❌ Incorrect
return fmt.Errorf("Failed to create directory: %w", err)  // Capital first letter
return fmt.Errorf("error: %w", err)  // Too vague
return fmt.Errorf("failed to create directory.\n", err)  // Period at end
```

### 5. Avoid Copy-Paste Error Messages

Error messages must accurately reflect what operation failed. Review error messages to ensure they describe the actual failing operation, not a copy-pasted message.

**Example of the issue:**
```go
// ❌ Misleading - error is from configToMap, not getting provider model
return nil, fmt.Errorf("get model for provider: %w", err)

// ✅ Correct - describe actual operation
return nil, fmt.Errorf("convert config to map: %w", err)
```

## Package-Specific Guidelines

### pkg/agent
- **Priority:** High
- **Issues:** Some errors use `%s` instead of `%w` for wrapping
- **Action:** Convert non-wrapping errors to wrapping where appropriate

### pkg/configuration
- **Priority:** High
- **Issues:** Copy-paste errors in `manager.go` and `custom_provider_registry.go`
- **Action:** Fix misleading error messages to match actual operations

### pkg/webui
- **Priority:** Medium
- **Issues:** Inconsistent HTTP error response patterns
- **Action:** Adopt `writeJSONErr` helper pattern consistently

## Common Patterns

### Error Checking
```go
result, err := someFunction()
if err != nil {
    return fmt.Errorf("operation: %w", err)
}
```

### Using errors.Is
```go
if errors.Is(err, filesystem.ErrOutsideWorkingDirectory) {
    // Handle security boundary error
}
if errors.Is(err, agent.ErrUINotAvailable) {
    // Handle unavailable UI
}
```

### Using errors.As
```go
var syntaxErr *json.SyntaxError
if errors.As(err, &syntaxErr) {
    // Handle JSON syntax error
}
```

### StructuredError with Builder
```go
err := utils.NewExecutionError("tool", "shell_command", originalErr).
    WithComponent("agent").
    WithOperation("execute").
    WithResource("command").
    WithMetadata(map[string]interface{}{
        "cwd": cwd,
    })
```

## References

- `pkg/utils/errors.go` - StructuredError implementation
- `pkg/agent/error_handler.go` - Error handling patterns for agent
- `pkg/filesystem/filesystem.go` - Security error handling
