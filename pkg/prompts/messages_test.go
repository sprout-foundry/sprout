package prompts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessageStruct(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		content  string
		expected Message
	}{
		{
			name:     "user message",
			role:     "user",
			content:  "Hello, world!",
			expected: Message{Role: "user", Content: "Hello, world!"},
		},
		{
			name:     "assistant message",
			role:     "assistant",
			content:  "How can I help?",
			expected: Message{Role: "assistant", Content: "How can I help?"},
		},
		{
			name:     "system message",
			role:     "system",
			content:  "You are a helpful assistant.",
			expected: Message{Role: "system", Content: "You are a helpful assistant."},
		},
		{
			name:     "empty content",
			role:     "user",
			content:  "",
			expected: Message{Role: "user", Content: ""},
		},
		{
			name:     "multiline content",
			role:     "assistant",
			content:  "Line 1\nLine 2\nLine 3",
			expected: Message{Role: "assistant", Content: "Line 1\nLine 2\nLine 3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := Message{
				Role:    tt.role,
				Content: tt.content,
			}
			if msg.Role != tt.expected.Role {
				t.Errorf("Message.Role = %q, want %q", msg.Role, tt.expected.Role)
			}
			if msg.Content != tt.expected.Content {
				t.Errorf("Message.Content = %q, want %q", msg.Content, tt.expected.Content)
			}
		})
	}
}

func TestMessageJSONMarshaling(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		wantErr bool
	}{
		{
			name:    "standard message",
			message: Message{Role: "user", Content: "Hello"},
			wantErr: false,
		},
		{
			name:    "message with special characters",
			message: Message{Role: "assistant", Content: "Quote: \"Hello\""},
			wantErr: false,
		},
		{
			name:    "empty content",
			message: Message{Role: "system", Content: ""},
			wantErr: false,
		},
		{
			name:    "multiline content",
			message: Message{Role: "user", Content: "Line 1\nLine 2\nLine 3"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test marshaling
			data, err := json.Marshal(tt.message)
			if (err != nil) != tt.wantErr {
				t.Fatalf("json.Marshal() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Test unmarshaling
			var unmarshaled Message
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if unmarshaled.Role != tt.message.Role {
				t.Errorf("Unmarshal Role = %q, want %q", unmarshaled.Role, tt.message.Role)
			}
			if unmarshaled.Content != tt.message.Content {
				t.Errorf("Unmarshal Content = %q, want %q", unmarshaled.Content, tt.message.Content)
			}
		})
	}
}

func TestMessageJSONFieldTags(t *testing.T) {
	// Verify JSON field names are correct
	msg := Message{Role: "user", Content: "test"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"role"`) {
		t.Error("JSON should use 'role' field name")
	}
	if !strings.Contains(jsonStr, `"content"`) {
		t.Error("JSON should use 'content' field name")
	}
}

func TestPotentialSecurityConcernsFound(t *testing.T) {
	tests := []struct {
		name         string
		relativePath string
		concern      string
		snippet      string
		contains     []string
	}{
		{
			name:         "basic security concern",
			relativePath: "config/secrets.yaml",
			concern:      "hardcoded API key",
			snippet:      "api_key: abc123",
			contains:     []string{"⚠️", "config/secrets.yaml", "hardcoded API key", "api_key: abc123", "Is this a security issue?"},
		},
		{
			name:         "SQL injection concern",
			relativePath: "db/queries.go",
			concern:      "potential SQL injection",
			snippet:      "db.Query(\"SELECT * FROM users WHERE id = \" + userID)",
			contains:     []string{"⚠️", "db/queries.go", "potential SQL injection", "SELECT * FROM users", "Is this a security issue?"},
		},
		{
			name:         "path traversal concern",
			relativePath: "handlers/files.go",
			concern:      "path traversal vulnerability",
			snippet:      "filepath.Join(\"/uploads\", userPath)",
			contains:     []string{"⚠️", "handlers/files.go", "path traversal vulnerability"},
		},
		{
			name:         "empty snippet",
			relativePath: "test.txt",
			concern:      "some concern",
			snippet:      "",
			contains:     []string{"⚠️", "test.txt", "some concern"},
		},
		{
			name:         "multiline snippet",
			relativePath: "auth/login.go",
			concern:      "weak password check",
			snippet:      "if len(password) < 4 {\n  return true\n}",
			contains:     []string{"⚠️", "auth/login.go", "weak password check", "password"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PotentialSecurityConcernsFound(tt.relativePath, tt.concern, tt.snippet)

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("PotentialSecurityConcernsFound() missing expected substring %q in result:\n%s", expected, result)
				}
			}
		})
	}
}

func TestPotentialSecurityConcernsFoundFormat(t *testing.T) {
	result := PotentialSecurityConcernsFound("path/to/file.go", "test concern", "code snippet")

	// Verify the structure of the output
	if !strings.HasPrefix(result, "⚠️  Potential security concern found in ") {
		t.Error("Output should start with warning emoji and proper prefix")
	}

	if !strings.Contains(result, "path/to/file.go: test concern") {
		t.Error("Output should contain path and concern on same line")
	}

	if !strings.HasSuffix(result, "\nIs this a security issue? (y/n)") {
		t.Error("Output should end with the yes/no question")
	}

	// The snippet should be on its own line
	parts := strings.Split(result, "\n")
	if len(parts) < 3 {
		t.Error("Output should have at least 3 lines (header, snippet, question)")
	}
}

func TestSkippingLLMSummarizationDueToSecurity(t *testing.T) {
	tests := []struct {
		name         string
		relativePath string
		contains     []string
	}{
		{
			name:         "standard path",
			relativePath: "config/database.yaml",
			contains:     []string{"⚠️", "Skipping LLM summarization", "config/database.yaml", "security concerns"},
		},
		{
			name:         "nested path",
			relativePath: "internal/secrets/credentials.json",
			contains:     []string{"⚠️", "Skipping LLM summarization", "internal/secrets/credentials.json"},
		},
		{
			name:         "simple filename",
			relativePath: ".env",
			contains:     []string{"⚠️", "Skipping LLM summarization", ".env"},
		},
		{
			name:         "empty path",
			relativePath: "",
			contains:     []string{"⚠️", "Skipping LLM summarization", "security concerns"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SkippingLLMSummarizationDueToSecurity(tt.relativePath)

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("SkippingLLMSummarizationDueToSecurity() missing expected substring %q in result: %s", expected, result)
				}
			}
		})
	}
}

func TestCodeReviewStagedPrompt(t *testing.T) {
	result := CodeReviewStagedPrompt()

	// Test that the prompt contains essential sections
	essentialSections := []string{
		"You are performing a thorough code review",
		"How to Use the Provided Context",
		"Project Type",
		"Commit Message (Intent)",
		"Review Focus Areas",
		"Logic & Correctness",
		"Security",
		"Performance",
		"Maintainability",
		"Best Practices",
		"Error Handling",
		"Testing",
		"Output Format",
		"Summary",
		"Issues by Severity",
		"CRITICAL",
		"HIGH",
		"MEDIUM",
		"LOW",
		"Positive Aspects",
		"Recommendations",
		"Conclusion",
	}

	for _, section := range essentialSections {
		if !strings.Contains(result, section) {
			t.Errorf("CodeReviewStagedPrompt() missing essential section: %q", section)
		}
	}
}

func TestCodeReviewStagedPromptSecurityGuidelines(t *testing.T) {
	result := CodeReviewStagedPrompt()

	// Test for security-specific guidelines
	securityGuidelines := []string{
		"Distinguish INTRODUCING vulnerabilities vs FIXING them",
		"injection",
		"authentication",
		"authorization",
		"Avoid False Positives",
	}

	for _, guideline := range securityGuidelines {
		if !strings.Contains(result, guideline) {
			t.Errorf("CodeReviewStagedPrompt() missing security guideline: %q", guideline)
		}
	}
}

func TestCodeReviewStagedPromptContextUsage(t *testing.T) {
	result := CodeReviewStagedPrompt()

	// Test that context usage instructions are present
	contextInstructions := []string{
		"Read these sections FIRST",
		"Understand the developer's intent",
		"Recognize security fixes vs vulnerabilities",
		"Key Code Comments",
		"Change Categories",
	}

	for _, instruction := range contextInstructions {
		if !strings.Contains(result, instruction) {
			t.Errorf("CodeReviewStagedPrompt() missing context instruction: %q", instruction)
		}
	}
}

func TestCodeReviewStagedPromptDependencyHandling(t *testing.T) {
	result := CodeReviewStagedPrompt()

	// Test for dependency file handling instructions
	dependencyInstructions := []string{
		"Dependency Files",
		"go.sum",
		"package-lock.json",
		"Don't review individual checksums",
	}

	for _, instruction := range dependencyInstructions {
		if !strings.Contains(result, instruction) {
			t.Errorf("CodeReviewStagedPrompt() missing dependency instruction: %q", instruction)
		}
	}
}

func TestCodeReviewStagedPromptOutputStructure(t *testing.T) {
	result := CodeReviewStagedPrompt()

	// Test that output format is well-defined
	outputFormatElements := []string{
		"[File:line]",
		"Why it matters",
		"Suggested fix",
		"approved",
		"needs revision",
		"rejected",
		"### Summary",
		"### Issues by Severity",
		"### Positive Aspects",
		"### Recommendations",
		"### Conclusion",
	}

	for _, element := range outputFormatElements {
		if !strings.Contains(result, element) {
			t.Errorf("CodeReviewStagedPrompt() missing output format element: %q", element)
		}
	}
}

func TestCodeReviewStagedPromptConsistency(t *testing.T) {
	// Test that multiple calls return the same result
	result1 := CodeReviewStagedPrompt()
	result2 := CodeReviewStagedPrompt()

	if result1 != result2 {
		t.Error("CodeReviewStagedPrompt() should return consistent results across calls")
	}

	// Test that result is not empty
	if result1 == "" {
		t.Error("CodeReviewStagedPrompt() should not return empty string")
	}

	// Test that result ends properly
	if !strings.HasSuffix(result1, "staged changes:") {
		t.Error("CodeReviewStagedPrompt() should end with 'staged changes:' to signal where diff will be placed")
	}
}

func TestCodeReviewStagedPromptLength(t *testing.T) {
	result := CodeReviewStagedPrompt()

	// The prompt should be substantial (at least 2000 characters)
	if len(result) < 2000 {
		t.Errorf("CodeReviewStagedPrompt() seems too short (%d chars), expected detailed instructions", len(result))
	}

	// But not excessively long (less than 10000 characters)
	if len(result) > 10000 {
		t.Errorf("CodeReviewStagedPrompt() seems too long (%d chars), may need trimming", len(result))
	}
}

func TestPromptFunctionsReturnNonEmpty(t *testing.T) {
	// All prompt functions should return non-empty strings
	functions := []struct {
		name     string
		fn       func() string
		minCalls int
	}{
		{"CodeReviewStagedPrompt", CodeReviewStagedPrompt, 1},
	}

	for _, tc := range functions {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.fn()
			if result == "" {
				t.Errorf("%s() returned empty string", tc.name)
			}
		})
	}
}
