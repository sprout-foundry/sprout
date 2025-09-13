# Testing Request Module

## Request Type: Testing & Validation

**Indicators**: "test", "tests", "testing", "unit test", "integration test", "verify", "validate", "check"

**Strategy**: Analyze test requirements → implement tests → verify functionality → ensure coverage

## Execution Approach

### Step 1: Test Analysis
- Understand what needs to be tested
- Identify test type (unit, integration, end-to-end)
- Review existing test patterns in codebase
- Check test framework and conventions

**Test Discovery Commands**:
```bash
# Find existing tests
find . -name "*_test.go" -o -name "*test*" | head -10

# Check test framework
grep -r "testing" --include="*.go" . | head -5

# Look for test patterns
grep -r "func Test" --include="*.go" .
```

### Step 2: Test Implementation
- Follow existing test patterns and conventions
- Write clear, focused test cases
- Include both positive and negative test cases
- Ensure proper test setup and cleanup

### Step 3: Test Execution
- Run tests to verify they work correctly
- Check test coverage if applicable
- Ensure tests fail when they should
- Verify tests pass with correct implementation

### Step 4: Test Validation
- Run the full test suite to ensure no regressions
- Check that new tests integrate well with existing ones
- Verify test output is clear and informative

## Testing Patterns

**Test Structure**: Follow Arrange-Act-Assert pattern
**Test Naming**: Use descriptive test names that explain what is being tested
**Test Data**: Use realistic test data and edge cases
**Error Testing**: Include tests for error conditions and edge cases

## Testing Rules
- **Test the Implementation**: Write tests for the actual code behavior
- **Clear Assertions**: Use specific assertions that clearly indicate what failed
- **Independent Tests**: Each test should be able to run independently
- **Meaningful Names**: Test names should describe the scenario being tested