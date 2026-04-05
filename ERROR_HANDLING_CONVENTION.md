# Error Handling Convention

This document defines the standardized error handling patterns for the ledit codebase.

## Core Principles

1. **Always provide context** - Every error should have meaningful context about what operation failed
2. **Use wrapping consistently** - When wrapping errors, always use the `%w` verb
3. **Prefer `fmt.Errorf` over `errors.New`** - For consistency, use `fmt.Errorf` even for simple messages

## Patterns

### ✅ Use This Pattern

```go
// Wrapping an error with context
return fmt.Errorf("failed to load config: %w", err)

// Simple error message without wrapping
return fmt.Errorf("invalid configuration value")

// Passing through an error when no additional context is needed
return err
```

### ❌ Avoid This Pattern

```go
// Simple error message with errors.New (inconsistent)
return errors.New("invalid configuration value")
```

## Rationale

- **Consistency:** Using `fmt.Errorf` for all error creation makes the codebase uniform
- **Flexibility:** `fmt.Errorf` works for both simple and complex error messages
- **Readability:** Consistent patterns make code easier to scan and understand
- **Best Practice:** The `%w` verb for wrapping errors is the Go standard for error wrapping

## Migration Notes

When migrating existing code:
- Replace `errors.New("message")` with `fmt.Errorf("message")`
- Keep existing `fmt.Errorf("message: %w", err)` patterns (they're already correct)
- Keep bare `return err` statements when no additional context is needed
