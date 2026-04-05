# Go Error Handling Guidelines

## Project-Wide Convention

This project follows a standardized error handling pattern to ensure consistency, maintainability, and proper error context propagation.

### Core Principles

1. **Always wrap errors with context** - Never return bare errors without context
2. **Use `fmt.Errorf` with `%w` verb for wrapping** - Enables error unwrapping with `errors.Is` and `errors.As`
3. **Use `errors.New` only for sentinel errors** - Package-level error variables that need identity comparison
4. **Provide meaningful context** - Describe what operation failed, not just "error occurred"

### Standard Patterns

#### 1. Wrapping Errors with Context

```go
// ❌ BAD - Bare error return
return err

// ❌ BAD - No wrapping
return fmt.Errorf("operation failed")

// ✅ GOOD - Wrap with context
return fmt.Errorf("failed to read config file: %w", err)

// ✅ GOOD - Wrap with additional context
return fmt.Errorf("failed to connect to %s: %w", host, err)
```

#### 2. Sentinel Errors (Package-Level)

```go
// ✅ GOOD - Sentinel error at package level
var ErrNotFound = errors.New("resource not found")
var ErrInvalidInput = errors.New("invalid input")

// Usage in functions
if notFound {
    return ErrNotFound
}

// Comparison
if errors.Is(err, ErrNotFound) {
    // handle not found
}
```

#### 3. Creating New Errors (Not Wrapping)

```go
// ✅ GOOD - Simple error without wrapping
return fmt.Errorf("invalid parameter: value must be positive")

// ✅ GOOD - For validation errors
return fmt.Errorf("%s is required: %w", fieldName, err)
```

#### 4. Multiple Error Checks

```go
// ✅ GOOD - Check and wrap at each step
if err := validateInput(input); err != nil {
    return fmt.Errorf("input validation failed: %w", err)
}

if err := processInput(input); err != nil {
    return fmt.Errorf("input processing failed: %w", err)
}
```

### Context Guidelines

When wrapping errors, provide context that answers:
- **What operation** was being performed?
- **What resource** was being accessed?
- **Why** does this matter to the user?

```go
// ❌ BAD - Vague context
return fmt.Errorf("error: %w", err)

// ✅ GOOD - Clear, specific context
return fmt.Errorf("failed to write to %s: %w", filepath, err)

// ✅ GOOD - Action-oriented context
return fmt.Errorf("failed to initialize database connection: %w", err)
```

### When to NOT Wrap

1. **Return sentinel errors directly** - Don't wrap `ErrNotFound` or similar sentinel errors
2. **At package boundaries** - Only wrap when adding meaningful context
3. **When error already has sufficient context** - Don't over-wrap

### Examples by Common Patterns

#### File Operations
```go
data, err := os.ReadFile(path)
if err != nil {
    return fmt.Errorf("failed to read file %s: %w", path, err)
}
```

#### Network Operations
```go
resp, err := http.Get(url)
if err != nil {
    return fmt.Errorf("failed to fetch %s: %w", url, err)
}
defer resp.Body.Close()
```

#### Configuration Loading
```go
config, err := loadConfig(path)
if err != nil {
    return fmt.Errorf("failed to load configuration from %s: %w", path, err)
}
```

#### API Calls
```go
result, err := client.CallAPI(ctx, params)
if err != nil {
    return fmt.Errorf("API call failed for operation %s: %w", operation, err)
}
```

### Testing Error Handling

```go
// ✅ GOOD - Test error wrapping behavior
func TestFunctionError(t *testing.T) {
    _, err := Function()
    if err == nil {
        t.Fatal("expected error")
    }

    // Test error message contains context
    if !strings.Contains(err.Error(), "failed to") {
        t.Errorf("error message missing context: %v", err)
    }

    // Test error unwrapping
    var expectedErr *SomeError
    if !errors.As(err, &expectedErr) {
        t.Errorf("error does not wrap expected type")
    }
}
```

## Migration Checklist

When refactoring existing code:

1. Find all `return err` statements and wrap with context
2. Ensure `fmt.Errorf` uses `%w` verb (not `%v`) for wrapping
3. Verify sentinel errors are package-level variables
4. Check error messages provide meaningful context
5. Test that error unwrapping still works

## References

- Go 1.13+ error wrapping: https://go.dev/blog/error-handling-and-go
- `errors.Is` and `errors.As`: https://pkg.go.dev/errors
