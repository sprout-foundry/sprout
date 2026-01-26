package spec

// SpecExtractionPrompt returns the prompt for extracting a canonical spec from conversation
func SpecExtractionPrompt() string {
	return `You are extracting a canonical specification from a conversation between a user and an AI assistant.

Your goal is to analyze the conversation and create a clear, unambiguous specification that captures what the user wants.

## Extraction Process

1. **Read the entire conversation** to understand:
   - What does the user ultimately want?
   - What features did they explicitly request?
   - What did they explicitly reject or exclude?
   - What constraints or preferences did they express?
   - How will they know it's working correctly?

2. **Extract the following fields**:

### Objective
A single, clear sentence that states what the user wants.
- Start with a verb: "Add...", "Implement...", "Fix...", "Refactor..."
- Be specific about what's being built/changed
- Include key constraints mentioned by user

### In Scope
List what's explicitly included in the specification.
- Features the user requested
- Implementation details discussed and agreed upon
- Necessary changes to complete the objective
- Reasonable implementation details (even if not explicitly stated)

### Out of Scope
List what's explicitly excluded from the specification.
- Features the user rejected ("No, don't add X")
- Alternatives the user declined ("I don't want OAuth")
- Features mentioned but not requested
- "Nice to have" items that aren't required
- Future enhancements mentioned

### Acceptance Criteria
List how we'll know the implementation is complete and correct.
- Specific behaviors that must work
- Test cases mentioned by user
- Performance or quality requirements stated
- Edge cases that must be handled

### Context
Any additional context that helps understand the request.
- Project type or domain
- Technical constraints mentioned
- User's expertise level or preferences
- Related work or dependencies

## Important Guidelines

- **Be Conservative**: When in doubt, leave it OUT of scope. The user can always expand scope later.
- **Be Specific**: Vague specs lead to scope creep. Extract concrete requirements.
- **Respect Exclusions**: If the user said "no X" or "don't include Y", put it in out_of_scope.
- **Capture Nuance**: If user said "keep it simple", that means avoid over-engineering in scope.
- **Reasonable Details**: Include implementation details that are necessary to complete the objective, even if not explicitly stated. For example, if implementing "login", password hashing is a reasonable detail even if user didn't mention it.

## Output Format

Return ONLY valid JSON in this exact format:

{
  "objective": "Clear objective statement",
  "in_scope": [
    "Specific feature 1",
    "Specific feature 2"
  ],
  "out_of_scope": [
    "Feature user rejected",
    "Alternative user declined"
  ],
  "acceptance": [
    "Test case 1",
    "Validation criteria 2"
  ],
  "context": "Additional context from conversation",
  "confidence": 0.85,
  "reasoning": "Brief explanation of how you derived this spec from the conversation"
}

## Confidence Score

Rate your confidence (0.0 to 1.0):
- 0.9-1.0: Spec is crystal clear from conversation
- 0.7-0.9: Spec is clear with minor ambiguities
- 0.5-0.7: Spec has significant ambiguities but best interpretation
- 0.3-0.5: Spec is very unclear, major assumptions made
- 0.0-0.3: Cannot extract meaningful spec

Now, extract the canonical specification from this conversation:`
}

// ScopeValidationPrompt returns the prompt for validating changes against a spec
func ScopeValidationPrompt() string {
	return `You are validating that code changes match the canonical specification.

Your goal is to identify scope violations: changes that go beyond what was agreed upon in the specification.

## Validation Process

1. **Read the specification first** to understand:
   - What is the objective?
   - What's explicitly in scope?
   - What's explicitly out of scope?
   - What are the acceptance criteria?

2. **Review the code changes** (git diff) to identify:
   - What files were added/modified?
   - What functions/classes were added?
   - What features were implemented?
   - What dependencies were added?

3. **Identify violations** by checking:
   - Are there features in the code that are NOT in the spec's in_scope?
   - Are there features that the user explicitly rejected (in out_of_scope)?
   - Are there features that go beyond the stated objective?
   - Are there unnecessary "enhancements" the user didn't request?
   - Did the implementation add complexity or features not needed for acceptance criteria?

## What is NOT a Violation

These are expected and should NOT be flagged:
- **Implementation details**: Helper functions, error handling, validation (necessary to complete objective)
- **Reasonable additions**: If spec says "implement login" and code includes password hashing, that's reasonable
- **Quality improvements**: Refactoring for clarity, adding comments, better error messages (as long as they don't add new features)
- **Necessary dependencies**: If implementing a feature requires a library, that's not a violation
- **Bug fixes in same area**: If fixing a bug in related code, that's acceptable

## What IS a Violation

Flag these as scope violations:
- **New features**: Features the user never requested
- **Explicitly rejected**: Features the user said "no" to
- **Over-engineering**: Adding complexity, abstraction, or features not needed for acceptance
- **Scope expansion**: "While I was here, I also added X" when X wasn't requested
- **Different approach**: Implementing something fundamentally different from what was discussed

## Output Format

Return ONLY valid JSON in this exact format:

{
  "in_scope": true,
  "violations": [
    {
      "file": "path/to/file.go",
      "line": 42,
      "type": "addition",
      "severity": "high",
      "description": "What was added (e.g., 'OAuthLogin function')",
      "why": "Why it's out of scope (e.g., 'OAuth was explicitly excluded from spec')"
    }
  ],
  "summary": "Brief summary of scope compliance (e.g., 'Changes are within scope. All additions support the stated objective.')",
  "suggestions": [
    "How to fix violations (e.g., 'Remove OAuthLogin function as OAuth was not requested')"
  ]
}

## Severity Levels

- **critical**: Major feature added that's completely out of scope
- **high**: Significant addition user didn't request
- **medium**: Minor enhancement or over-engineering
- **low**: Very minor expansion (e.g., extra logging, comments)

Now, validate these changes against the specification:`
}
