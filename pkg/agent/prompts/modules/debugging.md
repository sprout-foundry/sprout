# Debugging Request Module

## Request Type: Fixing & Troubleshooting

**Indicators**: "fix", "debug", "troubleshoot", "error", "failing", "broken", "issue"

**Strategy**: Analyze error → investigate root cause → fix source → verify

## Critical Debugging Methodology

### Step 1: Read the Error Message
- Compiler errors tell you exactly what's wrong
- Test output shows what failed and why
- Don't skip or skim error messages - they contain the solution

**Common Error Patterns**:
- `undefined: functionName` → Function missing or not imported
- `cannot find package` → Import or dependency issue  
- `syntax error at line X` → Code syntax problem
- `test failed: expected X got Y` → Logic mismatch
- `no such file` → Path or import issue

### Step 2: Investigate Root Cause
**For `undefined` errors**:
- Search codebase: `grep -r "func functionName" --include="*.go" .`
- Check if function exists in another file
- Verify imports and access patterns

**For test failures**:
- Read the source code being tested, not just the test file
- Compare test expectations vs actual code behavior
- Look for missing functions, wrong logic, incorrect return values

**For compilation errors**:
- Go to exact file and line mentioned in error
- Read surrounding context, not just the error line

### Step 3: Fix Source Code
- Address the actual error, not symptoms
- Fix source code, not tests (unless test is wrong)
- Make targeted changes based on error analysis
- Test each change to verify it works

### Step 4: Verification
- Run the failing command again to check if error is resolved
- Ensure fix doesn't break other functionality
- Run related tests to verify broader system still works

## Debugging Rules
- **Read First**: Always start by carefully reading the full error message
- **Root Cause**: Fix the underlying issue, not symptoms
- **Source Focus**: Fix implementation code, not tests (unless test is incorrect)
- **Targeted Changes**: Make specific changes based on error analysis