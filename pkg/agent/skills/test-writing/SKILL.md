---
name: test-writing
description: Guidelines for writing effective unit tests, integration tests, and test coverage. Use when creating tests.
---

# Test Writing Guidelines

Follow these guidelines when writing tests for this project.

## Test Structure

### Naming

- Test files: `name_test.go` (next to source file)
- Test functions: `TestFunctionName` or `TestScenario`
- Helper functions: `testHelperName` (unexported)
- Subtests: `t.Run("description", func(t *testing.T) {...})`

### Organization

```
func TestProcessUser(t *testing.T) {
    // Arrange - set up test data and mocks
    user := &User{ID: 1, Name: "Alice"}
    mockDB.On("Get", 1).Return(user, nil)

    // Act - call the function being tested
    result, err := ProcessUser(1)

    // Assert - verify the result
    require.NoError(t, err)
    require.Equal(t, "Alice", result.Name)
    mockDB.AssertExpectations(t)
}
```

## Test Types

### Unit Tests

- Test single function/method in isolation
- Mock dependencies
- Fast (< 100ms each)
- High coverage for complex logic

### Integration Tests

- Test multiple components together
- Use real dependencies (database, API)
- May be slower
- Test happy path + key error cases

### Table-Driven Tests

Use for multiple input/output combinations:

```go
func TestValidateEmail(t *testing.T) {
    tests := []struct {
        name    string
        email   string
        wantErr bool
    }{
        {"valid", "test@example.com", false},
        {"invalid no @", "testexample.com", true},
        {"invalid no domain", "test@", true},
        {"empty", "", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateEmail(tt.email)
            if tt.wantErr {
                require.Error(t, err)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

## Assertions

Use `testify/require` for assertions that must pass (fail fast):

```go
require.NoError(t, err)           // stops test on failure
require.Equal(t, expected, actual)
require.NotNil(t, obj)
require.Len(t, slice, 3)
require.Contains(t, str, "substring")
```

Use `testify/assert` for non-critical checks:

```go
assert.NoError(t, err)            // continues on failure
assert.True(t, condition, "optional message")
```

## Mocking

### Interface Mocks

```go
type MockDB struct {
    mock.Mock
}

func (m *MockDB) Get(id int) (*User, error) {
    args := m.Called(id)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*User), args.Error(1)
}
```

### Using Mocks

```go
func TestGetUser(t *testing.T) {
    mockDB := new(MockDB)
    service := NewUserService(mockDB)

    mockDB.On("Get", 1).Return(&User{ID: 1, Name: "Alice"}, nil)

    user, err := service.GetUser(1)

    require.NoError(t, err)
    require.Equal(t, "Alice", user.Name)
    mockDB.AssertExpectations(t)
}
```

## Test Coverage

- Aim for 80%+ coverage on business logic
- Cover edge cases and error paths
- Don't test trivial getters/setters
- Focus on complex logic

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Fixtures

- Use testdata/ directory for test files
- Create factory functions for complex objects
- Clean up after tests

```go
func createTestUser(t *testing.T) *User {
    return &User{
        ID:   uuid.New(),
        Name: "Test User " + t.Name(),
    }
}
```

## Best Practices

1. **One assertion per test** is ideal but not required
2. **Descriptive names**: `TestValidateEmail_RejectsInvalidFormat`
3. **Isolate tests**: no shared state between tests
4. **Clean up**: defer cleanup in setup
5. **Test behavior**, not implementation
6. **Keep tests fast**: mock I/O operations
7. **Use t.Cleanup()** for resource management

## Common Patterns

### Testing HTTP Handlers

```go
func TestHealthHandler(t *testing.T) {
    req := httptest.NewRequest("GET", "/health", nil)
    w := httptest.NewRecorder()

    HealthHandler(w, req)

    require.Equal(t, http.StatusOK, w.Code)
    require.Contains(t, w.Body.String(), "healthy")
}
```

### Testing Errors

```go
func TestDivide_ByZero(t *testing.T) {
    _, err := Divide(10, 0)
    require.Error(t, err)
    require.Contains(t, err.Error(), "division by zero")
}
```

## Running Tests

```bash
go test ./...              # all tests
go test -v ./...          # verbose
go test -run TestName     # specific test
go test -cover ./...      # with coverage
go test -race ./...       # race detector
```
