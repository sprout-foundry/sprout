// Package utils provides error handling utilities and conventions for the codebase.
// This file documents the project's error handling conventions.
package utils

/*
ERROR HANDLING CONVENTIONS

This document describes the standardized error handling patterns used throughout
the Go codebase. Following these conventions ensures consistency, clarity, and
proper error propagation.

================================================================================
1. WRAPPING ERRORS WITH CONTEXT
================================================================================

Use fmt.Errorf with the %w verb to wrap errors when adding context. This preserves
the error chain for errors.Is() and errors.As() to work correctly.

Pattern:
    return fmt.Errorf("context: %w", err)

Example:
    data, err := readFile(path)  // readFile is a hypothetical helper function
    if err != nil {
        return fmt.Errorf("failed to load configuration: %w", err)
    }

When to use:
- When you want to preserve the error chain
- When the caller might want to check the underlying error type
- When the operation failed and you're adding context about what was being done

================================================================================
2. STANDALONE ERRORS WITHOUT WRAPPING
================================================================================

Use errors.New() for errors that don't wrap another error. These are typically
new error types created for specific conditions.

Pattern:
    return errors.New("message")

Example:
    if user == nil {
        return errors.New("user not found")
    }

When to use:
- Creating new error types for specific conditions
- Errors that don't have an underlying cause
- Simple validation failures

================================================================================
3. SECONDARY ERROR CONTEXT (NON-WRAPPING)
================================================================================

When you need to include additional error information but are NOT wrapping it
(i.e., not preserving it in the error chain), use %v for the secondary error.
This is typically done in parenthetical context to provide debugging information
without affecting error handling logic.

Pattern:
    return fmt.Errorf("primary error: %w (secondary context: %v)", primaryErr, secondaryErr)

Example:
    // Primary error is restoreErr (wrapped), parseErr is just context
    return fmt.Errorf("edit would produce invalid JSON in %s and restore failed: %w (original parse error: %v)",
        path, restoreErr, parseErr)

When to use:
- When you have multiple errors but only one is the "primary" failure
- The secondary error is for debugging/logging, not for error handling
- You're explicitly NOT wrapping the secondary error (don't use %w)

================================================================================
4. ERROR FORMATTING SPECIFIERS
================================================================================

Use the following format specifiers for errors:

- %w  : Wrap an error (preserves error chain, required for errors.Is/As)
- %v  : Format error with default formatting (for secondary context)
- %s  : Format error as string (rarely needed for errors)
- %T  : Format error type (for debugging)

DO NOT use:
- %s with errors (use %v or %w instead)
- Bare error returns when wrapping would be clearer (with exceptions noted below)

================================================================================
5. INTENTIONAL BARE ERROR RETURNS
================================================================================

In some cases, returning a bare error (not wrapped) is intentional and documented.
For example, in pkg/webcontent/webcontent_cache.go:143, a bare error is returned
during a cache miss (when the cached file doesn't exist). This is acceptable when:
- The error is a simple sentinel value
- No additional context is needed
- The calling code handles it explicitly

Example (intentional bare return - cache miss pattern):
    if os.IsNotExist(err) {
        // Return bare error as a control flow signal - caller checks for os.IsNotExist
        return nil, err
    }

================================================================================
6. ERROR SEVERITY AND CATEGORIES
================================================================================

The StructuredError type provides additional context through severity levels
and categories:

Severity Levels:
- SeverityLow: User-facing errors, validation failures
- SeverityMedium: Recoverable errors, network issues
- SeverityHigh: System errors, execution failures
- SeverityCritical: Unrecoverable errors, data corruption

Categories:
- CategorySystem: System-level errors
- CategoryNetwork: Network-related errors
- CategoryFileSystem: File I/O errors
- CategoryConfiguration: Configuration errors
- CategoryValidation: Input validation errors
- CategoryExecution: Operation execution errors
- CategoryUser: User-facing errors

================================================================================
7. BEST PRACTICES
================================================================================

1. ALWAYS wrap errors when adding context:
   ✓ return fmt.Errorf("failed to process: %w", err)
   ✗ return fmt.Errorf("failed to process: %v", err)  // loses error chain

2. Use errors.New for simple, standalone errors:
   ✓ return errors.New("invalid input")
   ✗ return fmt.Errorf("invalid input")  // unnecessary

3. Include secondary errors with %v in parenthetical context:
   ✓ return fmt.Errorf("operation failed: %w (debug: %v)", primary, secondary)

4. Don't silently ignore errors:
   ✓ if err != nil { return err }
   ✗ if err != nil { log.Println(err) }  // loses error

5. Use WrapError() helper for simple wrapping:
   ✓ return WrapError(err, "additional context")

6. Check error types appropriately:
   ✓ if IsValidationError(err) { ... }
   ✓ if errors.Is(err, ErrNotFound) { ... }

================================================================================
8. EXAMPLES
================================================================================

Example 1: File operations with wrapping
    func ReadConfig(path string) (*Config, error) {
        data, err := os.ReadFile(path)
        if err != nil {
            return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
        }
        
        config, err := parseConfig(data)
        if err != nil {
            return nil, fmt.Errorf("failed to parse config: %w", err)
        }
        
        return config, nil
    }

Example 2: Multiple operations with secondary context
    func ProcessFile(path string) error {
        // Try page-based OCR
        pageErr := rasterizePage(path)
        
        // Try image-based OCR
        ocrErr := extractAndOCR(path)
        
        // Both failed - report primary error, secondary as context
        if pageErr != nil && ocrErr != nil {
            return fmt.Errorf("both OCR paths failed: page=%v, image=%w", pageErr, ocrErr)
        }
        
        return nil
    }

Example 3: Validation errors
    func ValidateUser(user *User) error {
        if user.Name == "" {
            return NewValidationError("name", "cannot be empty")
        }
        
        if !isValidEmail(user.Email) {
            return NewValidationError("email", "invalid format")
        }
        
        return nil
    }

Example 4: System errors with context
    func StartService() error {
        port, err := acquirePort(8080)
        if err != nil {
            return NewSystemError("acquire port", err)
        }
        return nil
    }

================================================================================
9. REFERENCES
================================================================================

- Go Errors FAQ: https://go.dev/doc/faq#errors
- Go Error Handling: https://go.dev/blog/error-handling-and-go
- Go 1.13 Error Wrapping: https://go.dev/blog/go1.13-errors

================================================================================
*/
