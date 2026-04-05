# Error Handling Convention for ledit

This document defines the standardized error handling patterns for the ledit project.

## Core Principles

1. **Always wrap errors with context** when they're returned from a function
2. **Use `%w` verb** for error wrapping to preserve error chain
3. **Use package-level error variables** for sentinel errors
4. **Keep error messages clear and actionable**

## Standard Patterns

### 1. Wrapping Errors from Other Functions

When returning an error from another function, always wrap it with context:

```go
// ✅ GOOD - Wrap with context
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
    }
    // ...
}

// ❌ BAD - Bare error return loses context
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    // ...
}
```

### 2. Creating New Errors

Use `errors.New()` for simple errors that don't wrap another error:

```go
// ✅ GOOD - Simple error
func ValidateInput(input string) error {
    if input == "" {
        return errors.New("input cannot be empty")
    }
    return nil
}

// ✅ ALSO GOOD - fmt.Errorf with formatting
func ValidateRange(value int, min, max int) error {
    if value < min || value > max {
        return fmt.Errorf("value %d is outside range [%d, %d]", value, min, max)
    }
    return nil
}
```

### 3. Package-Level Error Variables

Define sentinel errors as package-level variables:

```go
// ✅ GOOD - Package-level error
var ErrOutsideWorkingDirectory = errors.New("file access outside working directory")

func (fs *FileSystem) ReadFile(path string) ([]byte, error) {
    if !fs.isAllowed(path) {
        return nil, ErrOutsideWorkingDirectory
    }
    // ...
}
```

### 4. Validation Errors

Use `fmt.Errorf()` with format verbs for validation errors:

```go
// ✅ GOOD - Validation error with context
func ValidateWorkflowStep(step *Step) error {
    if step.Prompt == "" && step.PromptFile == "" {
        return fmt.Errorf("step %s requires prompt or prompt_file", step.Name)
    }
    return nil
}
```

### 5. Multiple Errors (Aggregation)

When collecting multiple errors, aggregate them:

```go
// ✅ GOOD - Error aggregation
func ValidateConfig(config *Config) error {
    var errs []error
    if config.Provider == "" {
        errs = append(errs, errors.New("provider is required"))
    }
    if config.Model == "" {
        errs = append(errs, errors.New("model is required"))
    }
    if len(errs) > 0 {
        return fmt.Errorf("config validation failed: %w", errors.Join(errs...))
    }
    return nil
}
```

## Acceptable Exceptions

### Simple Wrapper Functions

For functions that are simple wrappers where the context is obvious from the function name, bare error returns are acceptable:

```go
// ✅ ACCEPTABLE - Simple wrapper with clear context
func (ir *InputReader) fallbackReadLine() (string, error) {
    fmt.Print(ir.prompt)
    var input string
    _, err := fmt.Scanln(&input)
    return input, err
}

// ✅ ACCEPTABLE - Transparent wrapper
func (a *Agent) executeShellCommandWithTruncation(ctx context.Context, command string) (string, error) {
    fullResult, err := tools.ExecuteShellCommand(ctx, command)
    // ... truncation logic ...
    returnResult := fullResult
    return returnResult, err
}
```

## Migration Strategy

When migrating existing code:

1. **Keep validation errors as-is** - They're already using appropriate patterns
2. **Add context to bare error returns** unless they're simple wrappers
3. **Use `%w` verb** when wrapping errors
4. **Keep package-level error variables** as-is (they're already correct)

## Common Patterns to Avoid

```go
// ❌ BAD - Don't lose error context
func DoSomething() error {
    if err := someFunction(); err != nil {
        return err
    }
    return nil
}

// ❌ BAD - Don't mix %s and %w
func DoSomething() error {
    if err := someFunction(); err != nil {
        return fmt.Errorf("something went wrong: %s", err)
    }
    return nil
}

// ❌ BAD - Don't create sentinel errors that could be wrapped
var ErrFailed = errors.New("failed")
func DoSomething() error {
    if err := someFunction(); err != nil {
        return ErrFailed  // Loses underlying error
    }
    return nil
}
```

## Testing Error Behavior

When testing error handling, use `errors.Is()` and `errors.As()`:

```go
func TestValidatePath(t *testing.T) {
    err := ValidatePath("/etc/passwd")
    if !errors.Is(err, ErrOutsideWorkingDirectory) {
        t.Errorf("expected ErrOutsideWorkingDirectory, got %v", err)
    }
}

func TestLoadConfig(t *testing.T) {
    _, err := LoadConfig("/nonexistent/config.json")
    var pathErr *os.PathError
    if !errors.As(err, &pathErr) {
        t.Errorf("expected *os.PathError, got %T", err)
    }
}
```

## Summary

- **Always wrap errors** with context using `fmt.Errorf("context: %w", err)`
- **Use `errors.New()`** for simple errors and sentinel errors
- **Keep validation errors** as `fmt.Errorf()` with formatting
- **Package-level error variables** are appropriate for sentinel errors
- **Simple wrapper functions** may return errors directly
- **Test with `errors.Is()` and `errors.As()`** for proper error checking
