# Go Error Handling Convention

**Version:** 1.0
**Last Updated:** 2026-04-05
**Status:** Active

## Overview

This document defines the standardized error handling patterns for the ledit codebase. It provides clear, actionable guidelines for when to use each error handling pattern to ensure consistency, maintainability, and proper error context propagation throughout the codebase.

## Core Principles

1. **Always provide meaningful context** - Every error should explain what operation failed and include relevant identifiers (file paths, user IDs, URLs, etc.)
2. **Be intentional about wrapping** - Use `%w` when you want callers to inspect the wrapped error, use `%v` when you don't
3. **Minimize sentinel errors** - Only use package-level sentinel errors when callers need to distinguish between specific error conditions
4. **Consistency matters** - Following these patterns makes the codebase easier to understand, debug, and maintain
5. **Context at boundaries** - Add context when errors cross package boundaries

---

## When to Use Each Pattern

### 1. `fmt.Errorf("context: %w", err)` - Wrapping Errors

**Use this when:**
- Wrapping an error from another package
- You want to add meaningful context (what operation failed, what resources were involved)
- You want callers to be able to inspect the wrapped error using `errors.Is()` or `errors.As()`
- The error crosses a package boundary

**Example:**
```go
// ✅ Correct - wrapping with context
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
    }
    // ...
}

// ✅ Correct - adding identifiers
func GetUserByID(ctx context.Context, userID string) (*User, error) {
    user, err := db.QueryUser(ctx, userID)
    if err != nil {
        return nil, fmt.Errorf("getting user %s: %w", userID, err)
    }
    // ...
}

// ❌ Incorrect - missing context
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err  // No context - where did it fail?
    }
    // ...
}

// ❌ Incorrect - redundant context
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        // os.Open already includes the path in its error
        return nil, fmt.Errorf("opening file %s: %w", path, err)
    }
    // ...
}

// ✅ Better - add what you were doing, not what the error already says
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("reading config: %w", err)
    }
    // ...
}
```

**Key Point:** Using `%w` makes the wrapped error part of your API. Only use it when you're intentionally exposing the wrapped error to callers.

---

### 2. `fmt.Errorf("message")` - Standalone Errors

**Use this when:**
- Creating a new error without wrapping an existing error
- Creating inline error messages within functions
- You need formatting capabilities (e.g., including variable values in the message)
- The error doesn't need to be inspected by callers with `errors.Is()` or `errors.As()`

**Example:**
```go
// ✅ Correct - standalone error with formatting
func ValidateInput(input string) error {
    if input == "" {
        return fmt.Errorf("input cannot be empty")
    }
    if len(input) > 1000 {
        return fmt.Errorf("input too long: %d characters (max 1000)", len(input))
    }
    return nil
}

// ✅ Correct - validation error
func CheckPermissions(user User, action string) error {
    if !user.CanPerform(action) {
        return fmt.Errorf("user %s does not have permission to %s", user.ID, action)
    }
    return nil
}

// ❌ Avoid - errors.New for inline errors (inconsistent)
func ValidateInput(input string) error {
    if input == "" {
        return errors.New("input cannot be empty")  // Inconsistent
    }
    // ...
}

// ✅ Prefer - consistent use of fmt.Errorf
func ValidateInput(input string) error {
    if input == "" {
        return fmt.Errorf("input cannot be empty")  // Consistent
    }
    // ...
}
```

**Why `fmt.Errorf` over `errors.New` for inline errors?**
- Consistency: `fmt.Errorf` works for both simple and complex error messages
- Flexibility: If you later need to add formatting, you don't need to change the pattern
- Readability: Consistent patterns make the codebase easier to scan

---

### 3. `errors.New("message")` - Package-Level Sentinel Errors

**Use this when:**
- Creating package-level error variables that are exported or private
- Callers need to check for specific error conditions using `errors.Is()`
- The error has semantic meaning that callers should act on
- You need an error identity that can be compared

**Example:**
```go
// ✅ Correct - exported sentinel errors
var (
    ErrNotFound       = errors.New("resource not found")
    ErrInvalidInput   = errors.New("invalid input")
    ErrUnauthorized  = errors.New("unauthorized")
)

// ✅ Correct - private sentinel errors
var (
    errProviderStartupClosed = errors.New("provider startup cancelled by user")
    errSessionExpired       = errors.New("session expired")
)

// ✅ Correct - returning sentinel errors
func GetUser(ctx context.Context, id string) (*User, error) {
    user, err := db.QueryUser(ctx, id)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, ErrNotFound
        }
        return nil, fmt.Errorf("querying user %s: %w", id, err)
    }
    return user, nil
}

// ✅ Correct - callers checking for sentinel errors
user, err := GetUser(ctx, userID)
if errors.Is(err, ErrNotFound) {
    // Handle not found case
    return
}
if err != nil {
    // Handle other errors
    return err
}

// ❌ Incorrect - inline sentinel errors
func GetUser(ctx context.Context, id string) (*User, error) {
    user, err := db.QueryUser(ctx, id)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, errors.New("user not found")  // Don't do this
        }
        return nil, err
    }
    return user, nil
}
```

**Naming Conventions:**
- **Exported errors:** Start with `Err` (e.g., `ErrNotFound`, `ErrInvalidInput`)
- **Private errors:** Start with `err` (e.g., `errProviderStartupClosed`, `errSessionExpired`)

**When to avoid sentinel errors:**
- If you're just adding context to an error, use `fmt.Errorf` instead
- If the error is only used in one place, consider `fmt.Errorf` instead
- If you want to avoid exposing implementation details in your API

---

### 4. `return err` - Bare Error Return

**Use this when:**
- The error already has sufficient context from the calling function
- Passing through errors within the same package (caller knows what happened)
- The error is already at the right abstraction level
- The error is a sentinel error that should be returned as-is

**Example:**
```go
// ✅ Correct - same package, context already known
func ValidateAndSave(user User) error {
    if err := Validate(user); err != nil {
        return err  // Caller knows we're validating and saving
    }
    if err := Save(user); err != nil {
        return err  // Caller knows we're validating and saving
    }
    return nil
}

// ✅ Correct - returning sentinel errors
func GetConfig(path string) (*Config, error) {
    if path == "" {
        return nil, ErrInvalidInput
    }
    // ...
}

// ❌ Incorrect - crossing package boundary without context
func (c *Client) FetchUser(id string) (*User, error) {
    resp, err := c.http.Get(c.baseURL + "/users/" + id)
    if err != nil {
        return nil, err  // What URL? What operation?
    }
    // ...
}

// ✅ Correct - add context at package boundary
func (c *Client) FetchUser(id string) (*User, error) {
    resp, err := c.http.Get(c.baseURL + "/users/" + id)
    if err != nil {
        return nil, fmt.Errorf("fetching user %s: %w", id, err)
    }
    // ...
}

// ❌ Incorrect - same package but caller doesn't have context
func LoadConfigFile(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err  // Caller might not know which file failed
    }
    // ...
}

// ✅ Better - add file context even within same package
func LoadConfigFile(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("reading config file %q: %w", path, err)
    }
    // ...
}
```

**Rule of Thumb:**
- **Within the same package:** Use `return err` only if the caller is very close to the call site and has clear context about what operation failed
- **Across packages:** Always wrap with context using `fmt.Errorf("operation: %w", err)`

---

### 5. `fmt.Errorf("context: %v", err)` - Wrapping Without Unwrapping

**Use this when:**
- You want to add context for logging/debugging
- You don't want callers to inspect the wrapped error
- The wrapped error is an implementation detail
- You want to hide internal dependencies from your API

**Example:**
```go
// ✅ Correct - wrapping without exposing implementation
func (r *UserRepo) Get(ctx context.Context, id string) (*User, error) {
    var u User
    err := r.db.QueryRow(ctx, "SELECT ...", id).Scan(&u.Name, &u.Email)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, ErrNotFound
        }
        // %v: adds context for humans, but callers can't errors.Is to sql.ErrNoRows
        return nil, fmt.Errorf("querying user %s: %v", id, err)
    }
    return &u, nil
}

// ✅ Correct - at system boundaries (HTTP handlers)
func handleGetUser(w http.ResponseWriter, r *http.Request) {
    userID := r.URL.Query().Get("id")
    user, err := GetUser(r.Context(), userID)
    if err != nil {
        // %v: context for humans, no chain to expose internals
        http.Error(w, fmt.Sprintf("getting user: %v", err), http.StatusInternalServerError)
        return
    }
    // ...
}

// ❌ Incorrect - exposing implementation details
func (r *UserRepo) Get(ctx context.Context, id string) (*User, error) {
    var u User
    err := r.db.QueryRow(ctx, "SELECT ...", id).Scan(&u.Name, &u.Email)
    if err != nil {
        // %w: callers can now depend on sql.ErrNoRows, tying them to our database
        return nil, fmt.Errorf("querying user %s: %w", id, err)
    }
    return &u, nil
}
```

**Key Point:** Going from `%v` to `%w` is a backwards-compatible change, but going from `%w` to `%v` breaks callers who use `errors.Is` or `errors.As`. When in doubt, start with `%v`.

---

## Decision Tree

```
Are you returning an error?
├─ Is this a new error (not wrapping)?
│  └─ Is this a package-level variable that callers need to check?
│     ├─ Yes → Use errors.New("message")
│     └─ No  → Use fmt.Errorf("message")
│
└─ Are you wrapping an existing error?
   ├─ Are you crossing a package boundary?
   │  └─ Yes → Use fmt.Errorf("context: %w", err) or fmt.Errorf("context: %v", err)
   │            (Use %w if you want callers to inspect, %v otherwise)
   │
   └─ Are you within the same package?
      ├─ Does the caller have clear context about what failed?
      │  ├─ Yes → Use return err
      │  └─ No  → Use fmt.Errorf("context: %w", err)
```

---

## Error Context Guidelines

### What Context to Include

Always include information that helps answer:
- **What operation** was being performed? (e.g., "reading config", "fetching user", "creating file")
- **What resources** were involved? (e.g., file paths, URLs, user IDs, database names)
- **Why** does this matter? (e.g., "config file is required for startup")

**Good examples:**
```go
// ✅ Includes operation and resource
fmt.Errorf("failed to read config file %q: %w", path, err)

// ✅ Includes operation, resource, and why it matters
fmt.Errorf("required config file %q not found: %w", path, err)

// ✅ Clear action-oriented context
fmt.Errorf("failed to connect to database %s: %w", dbURL, err)

// ❌ Vague context
fmt.Errorf("error: %w", err)

// ❌ No context
return err
```

### Avoid Redundant Context

Don't include information that the underlying error already provides:

```go
// ❌ Redundant - os.Open already includes the path
return fmt.Errorf("opening file %s: %w", path, err)
// Result: "opening file /etc/config.yaml: open /etc/config.yaml: permission denied"

// ✅ Better - add what you were doing
return fmt.Errorf("reading config: %w", err)
// Result: "reading config: open /etc/config.yaml: permission denied"
```

---

## Summary Checklist

Before committing code, ask yourself:

- [ ] **Context:** Does every error have meaningful context about what operation failed?
- [ ] **Wrapping:** Am I using `%w` only when I want callers to inspect the wrapped error?
- [ ] **Sentinels:** Are `errors.New()` calls only at package level for sentinel errors?
- [ ] **Boundaries:** Am I adding context when errors cross package boundaries?
- [ ] **Consistency:** Am I using `fmt.Errorf` for inline errors (not `errors.New`)?
- [ ] **Redundancy:** Am I avoiding redundant information that's already in the error?
- [ ] **Tests:** Are my tests verifying error messages and unwrapping behavior?

---

## Quick Reference

| Pattern | When to Use | Example |
|---------|-------------|---------|
| `fmt.Errorf("context: %w", err)` | Wrapping errors, want callers to inspect | `return fmt.Errorf("reading config: %w", err)` |
| `fmt.Errorf("message")` | Inline errors, need formatting | `return fmt.Errorf("input cannot be empty")` |
| `errors.New("message")` | Package-level sentinel errors only | `var ErrNotFound = errors.New("not found")` |
| `return err` | Same package, caller has context | `return err` (only when appropriate) |
| `fmt.Errorf("context: %v", err)` | Wrapping without exposing implementation | `return fmt.Errorf("querying: %v", err)` |

---

## References and Further Reading

- **Go 1.13 Error Handling:** https://go.dev/blog/go1.13-errors
- **Error Values FAQ:** https://go.dev/wiki/ErrorValueFAQ
- **Dave Cheney - Don't just check errors:** https://dave.cheney.net/2016/04/27/dont-just-check-errors-handle-them-gracefully
- **Go Errors: to wrap or not to wrap?** https://rednafi.com/go/to-wrap-or-not-to-wrap/
- **Google Go Style Guide - Error Handling:** https://google.github.io/styleguide/go/best-practices.html#error-extra-info
- **wrapcheck Linter:** https://github.com/tomarrell/wrapcheck
