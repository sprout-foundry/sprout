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

// Converting error to string and back
return fmt.Errorf("%s", errStr)

// Missing %w when wrapping
return fmt.Errorf("operation failed", err)
```

## Detailed Guidelines

### 1. Sentinel Errors (Package-level)

Use `errors.New()` ONLY for package-level sentinel error variables:

```go
// ✅ Correct - package-level sentinel errors
var ErrNotFound = errors.New("not found")
var ErrInvalidInput = errors.New("invalid input")

// ❌ Incorrect - inline errors
func FindUser(id string) (*User, error) {
    return nil, errors.New("user not found") // Should use fmt.Errorf
}
```

### 2. Wrapping Errors

When wrapping errors to add context, **always** use `%w`:

```go
// ✅ Correct - proper wrapping with %w
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
    }
    return parseConfig(data)
}

// ❌ Incorrect - missing %w
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read config file %q, err: %v", path, err)
    }
    return parseConfig(data)
}
```

### 3. Simple Error Messages

For simple error messages without wrapping, prefer `fmt.Errorf` over `errors.New`:

```go
// ✅ Correct - consistent use of fmt.Errorf
func ValidateInput(input string) error {
    if input == "" {
        return fmt.Errorf("input cannot be empty")
    }
    return nil
}

// ❌ Inconsistent - mixing errors.New and fmt.Errorf
func ValidateInput(input string) error {
    if input == "" {
        return errors.New("input cannot be empty")
    }
    return fmt.Errorf("invalid format: %s", input)
}
```

### 4. Avoid String Conversion

Never convert errors to strings and back:

```go
// ❌ Incorrect - unnecessary string conversion
func isRateLimitError(errStr string) bool {
    return handleError(fmt.Errorf("%s", errStr))
}

// ✅ Correct - pass error directly if possible, or use a constant message
func isRateLimitError(errStr string) bool {
    return handleError(fmt.Errorf("rate limit check: %s", errStr))
}
```

### 5. Error Chaining

When you need to chain multiple errors, wrap each one properly:

```go
// ✅ Correct - proper error chaining
func ProcessFile(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return fmt.Errorf("failed to open file %q: %w", path, err)
    }
    defer file.Close()

    data, err := io.ReadAll(file)
    if err != nil {
        return fmt.Errorf("failed to read file %q: %w", path, err)
    }

    if err := validate(data); err != nil {
        return fmt.Errorf("failed to validate file %q: %w", path, err)
    }

    return nil
}
```

## Migration Notes

When migrating existing code:
- Replace `errors.New("message")` with `fmt.Errorf("message")`
- Keep existing `fmt.Errorf("message: %w", err)` patterns (they're already correct)
- Keep bare `return err` statements when no additional context is needed
- Replace `fmt.Errorf("%s", errStr)` with more meaningful error messages

## Rationale

- **Consistency:** Using `fmt.Errorf` for all error creation makes the codebase uniform
- **Flexibility:** `fmt.Errorf` works for both simple and complex error messages
- **Readability:** Consistent patterns make code easier to scan and understand
- **Best Practice:** The `%w` verb for wrapping errors is the Go standard for error wrapping
- **Error Inspection:** Using `%w` preserves the underlying error for `errors.Is()` and `errors.As()`

## Common Patterns by Context

### File Operations
```go
data, err := os.ReadFile(path)
if err != nil {
    return fmt.Errorf("failed to read file %q: %w", path, err)
}
```

### API Calls
```go
resp, err := client.Do(req)
if err != nil {
    return fmt.Errorf("failed to make API request to %s: %w", url, err)
}
```

### Configuration
```go
if value == "" {
    return fmt.Errorf("required configuration value is empty")
}
```

### User Input
```go
if input == "" {
    return fmt.Errorf("input is required")
}
```

## Testing Error Handling

When testing error handling, use `errors.Is()` and `errors.As()`:

```go
err := LoadConfig("nonexistent.json")
if errors.Is(err, os.ErrNotExist) {
    // Handle file not found
}

var pathErr *fs.PathError
if errors.As(err, &pathErr) {
    // Handle path-specific errors
}
```

## Summary

1. Use `errors.New()` only for package-level sentinel errors
2. Use `fmt.Errorf()` for all inline error creation
3. Always use `%w` when wrapping errors
4. Never do `fmt.Errorf("%s", errStr)` - provide meaningful context
5. Keep bare `return err` only when no additional context is needed
6. Document error handling decisions in code comments when non-obvious
