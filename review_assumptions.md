# Ledit PR Review Integration - Expected Behavior

## Overview

The PR review feature expects ledit to function as an AI agent that can analyze code diffs and provide structured feedback. Here's exactly how the integration expects ledit to behave:

## 1. Agent Invocation

**Command**: 
```bash
ledit agent --provider "$AI_PROVIDER" --model "$AI_MODEL" "$PROMPT"
```

**Expected Behavior**:
- Accept a detailed prompt describing the review task
- Use the specified provider and model via command-line flags
- Process files referenced in the prompt (context.md, full.diff)
- Output results to stdout

## 2. File Reading Capabilities

The agent is expected to read files when instructed in the prompt:

**Files we create**:
- `/tmp/ledit-pr-{number}/context.md` - PR metadata and instructions
- `/tmp/ledit-pr-{number}/full.diff` - The complete PR diff
- `/tmp/ledit-pr-{number}/files.txt` - List of changed files

**Expected agent behavior**:
```
"The PR context and diff have been saved to: $PR_DATA_DIR/context.md and $PR_DATA_DIR/full.diff"
"Start by reading the context and diff files."
```

The agent should:
1. Use its file reading tools to access these files
2. Parse the diff format to understand changes
3. Analyze the code changes in context

## 3. Output Format Requirements

The prompt asks for a specific two-part output:

### Part 1: JSON Structure
```json
{
  "summary": "Overall assessment of the PR",
  "approval_status": "approve|request_changes|comment",
  "comments": [
    {
      "file": "path/to/file.js",
      "line": 42,
      "side": "RIGHT",
      "body": "Your comment here",
      "severity": "critical|major|minor|suggestion"
    }
  ],
  "general_feedback": "Additional feedback not tied to specific lines"
}
```

### Part 2: Human-Readable Summary
After the JSON, a markdown-formatted summary for the PR comment.

**Expected ledit behavior**:
- Output valid JSON first
- Follow with human-readable content
- Understand diff line numbers and map them correctly
- Respect the review type and threshold parameters

## 4. Diff Analysis Capabilities

The agent needs to understand unified diff format:

```diff
--- a/src/example.js
+++ b/src/example.js
@@ -10,7 +10,7 @@
 function example() {
-  const oldCode = true;
+  const newCode = false;
   return result;
 }
```

**Expected capabilities**:
- Parse diff headers to identify files
- Understand line numbers from diff hunks
- Distinguish between added/removed/context lines
- Map comments to the correct line numbers
- Use "RIGHT" side for new code, "LEFT" for old code

## 5. Review Intelligence

Based on the `review-type` parameter, the agent should adjust its focus:

- **comprehensive**: All aspects of code quality
- **security**: SQL injection, XSS, auth issues, secrets
- **performance**: O(n²) algorithms, unnecessary loops, memory leaks
- **style**: Naming conventions, formatting, code organization

**Expected behavior**:
- Understand common patterns and anti-patterns
- Provide actionable feedback, not just observations
- Respect the severity levels appropriately

## 6. Cost Reporting

The script looks for this pattern in the output:
```
💰 Session: 228.8K total (228.8K processed + 0 cached) | $0.059806
```

**Expected**:
- Ledit outputs token usage and cost information
- Uses the 💰 emoji as a marker
- Includes dollar amount

## 7. Error Handling

The integration expects ledit to:
- Exit with non-zero code on failures
- Handle authentication errors (401/403)
- Work within the specified timeout
- Not require interactive input

## 8. Key Assumptions

1. **Working Directory**: The agent can access files using absolute paths
2. **Tool Usage**: The agent has tools for reading files and analyzing code
3. **Context Window**: Can handle reasonably sized diffs (most PRs)
4. **Output Parsing**: Outputs structured data that can be parsed
5. **Deterministic**: Given the same input, provides consistent quality reviews

## Testing Recommendations

To verify ledit works as expected:

1. **Test file reading**:
   ```bash
   echo "Test content" > /tmp/test.txt
   ledit agent --provider deepinfra --model some-model "Read the file /tmp/test.txt and tell me what it contains"
   ```

2. **Test JSON output**:
   ```bash
   ledit agent --provider deepinfra --model some-model "Output a JSON object with a 'status' field set to 'success'"
   ```

3. **Test diff understanding**:
   ```bash
   # Create a simple diff file and ask ledit to analyze it
   ```

## Potential Issues to Check

1. **File Path Access**: Ensure ledit can read absolute paths
2. **JSON Formatting**: Verify clean JSON output without markdown code blocks
3. **Memory/Context**: Check if large diffs cause issues
4. **Line Number Accuracy**: Ensure diff line mapping is correct
5. **Tool Availability**: Confirm file reading tools are available in agent mode

This integration assumes ledit is a capable code analysis tool that can understand context, read files, analyze diffs, and provide structured feedback. The key is that it needs to output in the expected format and handle the file-based workflow we've designed.