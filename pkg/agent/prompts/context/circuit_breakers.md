# Circuit Breaker Context Module

## Infinite Loop Prevention

### Circuit Breaker Rules (MANDATORY)

**File Editing Circuit Breaker**:
- If you edit the same file 3+ times without progress: STOP
- Re-read the error message carefully - you may have misunderstood it
- Search the codebase for missing functions or patterns
- Read related files that might contain missing pieces

**Test Command Circuit Breaker**:
- If you run the same failing test 4+ times without progress: STOP
- Analyze what the test is actually testing
- Check if you're fixing the right code vs just the test file
- Look for missing dependencies or setup issues

**General Circuit Breaker Logic**:
- Ask yourself: "Am I fixing the root cause or just symptoms?"
- If same approach fails 2-3 times, try a different strategy
- Step back and analyze the broader context
- Search for similar patterns in the codebase

### Recovery Actions

**When Circuit Breaker Triggers**:
1. **STOP** current approach immediately
2. **Re-read** the original error message from the beginning
3. **Search** the codebase for the missing pieces
4. **Investigate** related files and dependencies
5. **Ask** fundamental questions about what you're trying to accomplish

**Example Recovery Workflow**:
```
Circuit Breaker: Edited user_test.go 3 times, still failing with "undefined: validateUser"
→ STOP editing the test
→ Search: grep -r "func validateUser" --include="*.go" .
→ Found: validateUser exists in user.go but not exported
→ Fix: Export the function or create wrapper
```

### Prevention Strategies
- **Read Errors Carefully**: Most infinite loops come from not reading error messages
- **Search Before Editing**: Look for existing functions/patterns first
- **Fix Root Causes**: Address the actual problem, not symptoms
- **Test Incrementally**: Make small changes and test each one