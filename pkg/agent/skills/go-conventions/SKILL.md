---
name: go-conventions
description: Go coding conventions, best practices, and style guidelines. Use when writing or reviewing Go code.
---

# Go Conventions

Follow these conventions when writing Go code for this project.

## Code Organization

- Use `package` at the top of every file
- Group imports: stdlib, external, internal
- Order functions: exported first, then unexported
- Keep files under 500 lines

## Naming Conventions

- **Variables**: `camelCase` (e.g., `userName`, `maxRetries`)
- **Constants**: `PascalCase` for exported, `camelCase` for unexported (e.g., `MaxRetries`, `defaultTimeout`)
- **Functions**: `PascalCase` for exported (e.g., `GetUser`), `camelCase` for unexported (e.g., `processData`)
- **Types**: `PascalCase` (e.g., `UserService`, `Config`)
- **Interfaces**: `PascalCase`, often with `-er` suffix (e.g., `Reader`, `Writer`)

## Error Handling

- Return errors as last return value
- Wrap errors with context using `fmt.Errorf` or `errors.Wrap`
- Handle errors at call site, don't ignore with `_`
- Use sentinel errors for known conditions

```go
// Good
if err != nil {
    return fmt.Errorf("failed to process user %d: %w", userID, err)
}

// Bad
_ = mightFail()  // Never ignore errors
```

## Error Messages

- Use lowercase, no punctuation at end
- Include context: "failed to X because Y"
- Don't capitalize unless proper noun

## Testing

- Test files: `name_test.go` alongside `name.go`
- Test functions: `TestFunctionName`
- Table-driven tests preferred for multiple cases
- Use `require`/`assert` from testify

```go
func TestProcessUser(t *testing.T) {
    tests := []struct {
        name    string
        input   User
        want    Result
        wantErr bool
    }{
        {"valid user", user, result, false},
        {"invalid email", invalidUser, nil, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ProcessUser(tt.input)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            require.Equal(t, tt.want, got)
        })
    }
}
```

## Context Usage

- Pass `context.Context` as first argument
- Check for cancellation periodically
- Use `context.TODO()` when uncertain

```go
func FetchUser(ctx context.Context, id int) (*User, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", "/users/"+strconv.Itoa(id), nil)
    // ...
}
```

## Concurrency

- Use `sync.WaitGroup` for coordinating goroutines
- Use channels for data flow, not shared memory
- Check for race conditions with `-race` flag
- Don't leak goroutines: ensure all paths return or exit

## Logging

- Use structured logging with context
- Include relevant fields: user ID, operation, duration
- Don't log sensitive data (passwords, tokens)

## Configuration

- Use environment variables or config files
- Provide sensible defaults
- Validate on startup

## Imports

```go
import (
    // stdlib
    "context"
    "fmt"
    "io"
    "os"

    // external
    "github.com/pkg/errors"
    "github.com/stretchr/testify/require"

    // internal
    "github.com/alantheprice/ledit/pkg/db"
    "github.com/alantheprice/ledit/pkg/service"
)
```

## Common Patterns

- `init()` for one-time setup only
- Constructor functions: `NewThing() *Thing`
- Option pattern for configurable types
- Functional options for extensibility
