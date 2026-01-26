package prompts

// Message represents a conversation message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Security-related prompts
func PotentialSecurityConcernsFound(relativePath, concern, snippet string) string {
	return "⚠️  Potential security concern found in " + relativePath + ": " + concern + "\n" + snippet + "\nIs this a security issue? (y/n)"
}

func SkippingLLMSummarizationDueToSecurity(relativePath string) string {
	return "⚠️  Skipping LLM summarization for " + relativePath + " due to potential security concerns"
}

// CodeReviewStagedPrompt returns the prompt for code review
func CodeReviewStagedPrompt() string {
	return `You are performing a thorough code review of staged Git changes. Your goal is to identify potential issues, security vulnerabilities, bugs, and opportunities for improvement.

## How to Use the Provided Context

You will receive several sections of context BEFORE the actual diff. Use them in this order:

1. **Project Type**: Understand the language and ecosystem (Go, Node.js, etc.)
2. **Commit Message (Intent)**: What the changes are TRYING to accomplish
3. **Key Code Comments**: Important comments that explain WHY specific changes were made
4. **Change Categories**: High-level overview of what types of changes are included

**IMPORTANT**: Read these sections FIRST before looking at the diff. They will help you:
- Understand the developer's intent
- Recognize security fixes vs vulnerabilities
- Identify intentional refactoring vs bugs
- Avoid false positives

## Review Focus Areas

1. **Logic & Correctness**: Look for bugs, edge cases, incorrect assumptions, race conditions, and logic errors
2. **Security**: Identify potential security vulnerabilities (injection, authentication, authorization, data exposure)
   - Distinguish INTRODUCING vulnerabilities vs FIXING them
3. **Performance**: Find performance bottlenecks, inefficient algorithms, unnecessary allocations
4. **Maintainability**: Assess code readability, complexity, duplication, and adherence to DRY principles
5. **Best Practices**: Check for language/framework idioms, design patterns, and conventions
6. **Error Handling**: Ensure proper error handling, validation, and edge case coverage
7. **Testing**: Consider whether changes need tests or if existing tests need updates

## Important Guidelines

- **Be Specific**: Reference exact file paths and line numbers when providing feedback
- **Be Actionable**: Provide concrete suggestions for improvements, not just vague comments
- **Consider Context**: Understand the purpose of changes before criticizing
  - Read the "Key Code Comments" section to understand WHY changes were made
  - Check the "Change Categories" to see if this is marked as a security fix
  - Distinguish between FIXING a bug vs INTRODUCING one
  - Consider that code refactoring might intentionally remove/reorganize functionality
- **Prioritize Severity**: Label issues as: CRITICAL, HIGH, MEDIUM, LOW, or NITPICK
- **Dependency Files**: For lock files (go.sum, package-lock.json, etc.), only note major version changes or security concerns. Don't review individual checksums.
- **Generated Files**: Ignore minified files, bundle files, and auto-generated code
- **Balance**: Don't overwhelm with minor style issues if there are critical problems to fix
- **Avoid False Positives**:
  - If code was removed, check if functionality was intentionally refactored or moved elsewhere
  - If a function call was removed, check if the functionality is now handled differently
  - Don't flag security fixes as vulnerabilities - they're solutions, not problems
  - For version bumps, understand semantic versioning (MAJOR.MINOR.PATCH)
  - Trust the "Key Code Comments" - they explain the developer's intent
  - If a comment says "CRITICAL: ... we CANNOT do X", believe it - that's a constraint being fixed

## Output Format

Structure your review as follows:

### Summary
[Brief 2-3 sentence overview of the changes and their quality]

### Issues by Severity

**CRITICAL** (if any):
- [File:line] Issue description - Why it matters + Suggested fix

**HIGH** (if any):
- [File:line] Issue description - Why it matters + Suggested fix

**MEDIUM** (if any):
- [File:line] Issue description + Suggested improvement

**LOW** (if any):
- [File:line] Minor suggestions or nice-to-have improvements

### Positive Aspects
[Highlight what was done well - good patterns, clean code, thoughtful design]

### Recommendations
[Specific actionable suggestions ranked by priority]

### Conclusion
[Overall assessment: approved / needs revision / rejected]
[If needs revision or rejected: what specific changes must be made?]

Now, please review the following staged changes:`
}
