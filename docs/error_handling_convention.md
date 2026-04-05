# Go Error Handling Convention

## Standard Pattern

All error returns should wrap errors with context using `fmt.Errorf("context: %w", err)`.

### When to Wrap Errors

Wrap errors when:
- Returning an error from any function (except top-level main or test functions)
- The error originates from system calls (file I/O, network operations, etc.)
- The error originates from external libraries or API calls
- The error needs additional context for debugging

### When NOT to Wrap Errors

Do NOT wrap errors when:
- Returning predefined error constants (sentinel errors)
- The error already has sufficient context from a higher-level function
- The error is a well-known error constant defined in the same package

## Examples

### ✅ Good: Wrapping with Context

```go
// File operations
if err := os.ReadFile(path); err != nil {
    return fmt.Errorf("failed to read file %s: %w", path, err)
}

// HTTP operations
if err := http.NewRequest("GET", url, nil); err != nil {
    return fmt.Errorf("failed to create HTTP request for %s: %w", url, err)
}

// Shell commands
if err := cmd.Run(); err != nil {
    return fmt.Errorf("failed to execute command %q: %w", cmd.String(), err)
}
```

### ✅ Good: Sentinel Errors (No Wrapping)

```go
// Predefined error constants
var ErrOutsideWorkingDirectory = errors.New("file access outside working directory")

// Returning sentinel errors
if !isInWorkingDirectory(path) {
    return ErrOutsideWorkingDirectory
}
```

### ❌ Bad: Bare Error Returns

```go
// Missing context
if err := os.ReadFile(path); err != nil {
    return err
}

// Better:
if err := os.ReadFile(path); err != nil {
    return fmt.Errorf("failed to read file %s: %w", path, err)
}
```

## Context Guidelines

- **File operations**: Include the file path in the error message
- **Network operations**: Include URLs, endpoints, or hostnames
- **Shell commands**: Include the command string or command description
- **Tool handlers**: Include the tool name or operation being performed
- **API calls**: Include endpoint, resource, or operation context

## Error Message Format

Use lowercase for the first word of error messages (unless proper noun):
- `fmt.Errorf("failed to read file %s: %w", path, err)` ✅
- `fmt.Errorf("Failed to read file %s: %w", path, err)` ❌

Keep messages concise but informative:
- `fmt.Errorf("read failed: %w", err)` ❌ (too vague)
- `fmt.Errorf("failed to read config file: %w", err)` ✅ (specific)

## Testing

Test functions may use simpler error patterns for brevity, but production code must follow these conventions.
