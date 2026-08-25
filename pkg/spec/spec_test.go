package spec

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Prompt Functions
// ---------------------------------------------------------------------------

func TestSpecExtractionPrompt(t *testing.T) {
	prompt := SpecExtractionPrompt()

	t.Run("returns non-empty string", func(t *testing.T) {
		if prompt == "" {
			t.Fatal("SpecExtractionPrompt returned empty string")
		}
	})

	t.Run("contains expected key phrases", func(t *testing.T) {
		expectedPhrases := []string{
			"canonical specification",
			"objective",
			"in_scope",
			"out_of_scope",
			"acceptance",
			"confidence",
			"reasoning",
		}
		for _, phrase := range expectedPhrases {
			if !strings.Contains(strings.ToLower(prompt), strings.ToLower(phrase)) {
				t.Errorf("prompt missing expected phrase %q", phrase)
			}
		}
	})

	t.Run("contains JSON output format example", func(t *testing.T) {
		if !strings.Contains(prompt, "{") || !strings.Contains(prompt, "}") {
			t.Error("prompt should contain JSON output format")
		}
		if !strings.Contains(prompt, `"objective"`) {
			t.Error("prompt should contain objective JSON key")
		}
	})

	t.Run("contains confidence score guidance", func(t *testing.T) {
		if !strings.Contains(prompt, "0.9-1.0") && !strings.Contains(prompt, "confidence") {
			t.Error("prompt should contain confidence score guidance")
		}
	})
}

func TestScopeValidationPrompt(t *testing.T) {
	prompt := ScopeValidationPrompt()

	t.Run("returns non-empty string", func(t *testing.T) {
		if prompt == "" {
			t.Fatal("ScopeValidationPrompt returned empty string")
		}
	})

	t.Run("contains expected key phrases", func(t *testing.T) {
		expectedPhrases := []string{
			"scope violations",
			"in_scope",
			"out_of_scope",
			"violations",
			"severity",
		}
		for _, phrase := range expectedPhrases {
			if !strings.Contains(strings.ToLower(prompt), strings.ToLower(phrase)) {
				t.Errorf("prompt missing expected phrase %q", phrase)
			}
		}
	})

	t.Run("contains output format with severity levels", func(t *testing.T) {
		if !strings.Contains(prompt, "critical") || !strings.Contains(prompt, "high") ||
			!strings.Contains(prompt, "medium") || !strings.Contains(prompt, "low") {
			t.Error("prompt should contain severity level definitions")
		}
	})

	t.Run("contains what-is-not-a-violation section", func(t *testing.T) {
		if !strings.Contains(prompt, "NOT a Violation") {
			t.Error("prompt should contain 'NOT a Violation' section")
		}
	})
}

// ---------------------------------------------------------------------------
// Message JSON Roundtrip
// ---------------------------------------------------------------------------

func TestMessage_JSON(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "Hello, world!",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Role != msg.Role {
		t.Errorf("Role: expected %q, got %q", msg.Role, got.Role)
	}
	if got.Content != msg.Content {
		t.Errorf("Content: expected %q, got %q", msg.Content, got.Content)
	}
}

func TestMessage_JSON_EmptyFields(t *testing.T) {
	msg := Message{}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Role != "" {
		t.Errorf("expected empty Role, got %q", got.Role)
	}
	if got.Content != "" {
		t.Errorf("expected empty Content, got %q", got.Content)
	}
}

// ---------------------------------------------------------------------------
// CanonicalSpec JSON Roundtrip
// ---------------------------------------------------------------------------

func TestCanonicalSpec_JSON(t *testing.T) {
	spec := &CanonicalSpec{
		ID:         "spec-123-456",
		CreatedAt:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		UserPrompt: "Build a login page",
		Objective:  "Create a user login page",
		InScope:    []string{"Login form", "Validation"},
		OutOfScope: []string{"OAuth", "Social login"},
		Acceptance: []string{"User can login with valid credentials"},
		Context:    "React app with Go backend",
		Conversation: []Message{
			{Role: "user", Content: "I need a login page"},
			{Role: "assistant", Content: "Sure, I can help with that"},
		},
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got CanonicalSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	assertJSONRoundtrip(t, "ID", spec.ID, got.ID)
	assertJSONRoundtrip(t, "CreatedAt", spec.CreatedAt, got.CreatedAt)
	assertJSONRoundtrip(t, "UserPrompt", spec.UserPrompt, got.UserPrompt)
	assertJSONRoundtrip(t, "Objective", spec.Objective, got.Objective)
	assertJSONRoundtrip(t, "InScope", spec.InScope, got.InScope)
	assertJSONRoundtrip(t, "OutOfScope", spec.OutOfScope, got.OutOfScope)
	assertJSONRoundtrip(t, "Acceptance", spec.Acceptance, got.Acceptance)
	assertJSONRoundtrip(t, "Context", spec.Context, got.Context)
	assertJSONRoundtrip(t, "Conversation", spec.Conversation, got.Conversation)
}

func TestCanonicalSpec_JSON_NilSlices(t *testing.T) {
	spec := &CanonicalSpec{
		ID:           "spec-789",
		Objective:    "Test with nil slices",
		InScope:      nil,
		OutOfScope:   nil,
		Acceptance:   nil,
		Conversation: nil,
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got CanonicalSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.InScope != nil {
		t.Error("expected nil InScope, got non-nil")
	}
	if got.OutOfScope != nil {
		t.Error("expected nil OutOfScope, got non-nil")
	}
	if got.Acceptance != nil {
		t.Error("expected nil Acceptance, got non-nil")
	}
	if got.Conversation != nil {
		t.Error("expected nil Conversation, got non-nil")
	}
}

func TestCanonicalSpec_JSON_EmptyStrings(t *testing.T) {
	spec := &CanonicalSpec{
		ID:           "",
		UserPrompt:   "",
		Objective:    "",
		Context:      "",
		InScope:      []string{},
		OutOfScope:   []string{},
		Acceptance:   []string{},
		Conversation: []Message{},
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got CanonicalSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.ID != "" {
		t.Error("expected empty ID")
	}
	if got.UserPrompt != "" {
		t.Error("expected empty UserPrompt")
	}
	if got.Objective != "" {
		t.Error("expected empty Objective")
	}
	if got.Context != "" {
		t.Error("expected empty Context")
	}
	if len(got.InScope) != 0 {
		t.Errorf("expected empty InScope, got %d items", len(got.InScope))
	}
}

// ---------------------------------------------------------------------------
// SpecExtractionResult JSON Roundtrip
// ---------------------------------------------------------------------------

func TestSpecExtractionResult_JSON(t *testing.T) {
	spec := &CanonicalSpec{
		ID:         "spec-result-test",
		CreatedAt:  time.Now(),
		UserPrompt: "Test extraction result",
		Objective:  "Test objective",
	}

	result := &SpecExtractionResult{
		Spec:       spec,
		Confidence: 0.95,
		Reasoning:  "Clear requirements from conversation",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got SpecExtractionResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Confidence != result.Confidence {
		t.Errorf("Confidence: expected %f, got %f", result.Confidence, got.Confidence)
	}
	if got.Reasoning != result.Reasoning {
		t.Errorf("Reasoning: expected %q, got %q", result.Reasoning, got.Reasoning)
	}
	if got.Spec == nil {
		t.Fatal("expected non-nil Spec")
	}
	if got.Spec.ID != result.Spec.ID {
		t.Errorf("Spec.ID: expected %q, got %q", result.Spec.ID, got.Spec.ID)
	}
}

// ---------------------------------------------------------------------------
// ScopeViolation JSON Roundtrip
// ---------------------------------------------------------------------------

func TestScopeViolation_JSON(t *testing.T) {
	violation := ScopeViolation{
		File:        "auth/login.go",
		Line:        42,
		Type:        "addition",
		Severity:    "high",
		Description: "OAuth login function added",
		Why:         "OAuth was explicitly excluded from spec",
	}

	data, err := json.Marshal(violation)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ScopeViolation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	assertJSONRoundtrip(t, "File", violation.File, got.File)
	assertJSONRoundtrip(t, "Line", violation.Line, got.Line)
	assertJSONRoundtrip(t, "Type", violation.Type, got.Type)
	assertJSONRoundtrip(t, "Severity", violation.Severity, got.Severity)
	assertJSONRoundtrip(t, "Description", violation.Description, got.Description)
	assertJSONRoundtrip(t, "Why", violation.Why, got.Why)
}

func TestScopeViolation_JSON_AllSeverities(t *testing.T) {
	severities := []string{"critical", "high", "medium", "low"}
	for _, sev := range severities {
		t.Run("severity="+sev, func(t *testing.T) {
			violation := ScopeViolation{
				File:        "test.go",
				Line:        1,
				Type:        "addition",
				Severity:    sev,
				Description: "test",
				Why:         "test",
			}

			data, err := json.Marshal(violation)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}

			var got ScopeViolation
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if got.Severity != sev {
				t.Errorf("Severity: expected %q, got %q", sev, got.Severity)
			}
		})
	}
} // ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertJSONRoundtrip(t *testing.T, field string, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%s: expected %v, got %v", field, want, got)
	}
}

// ---------------------------------------------------------------------------
// CanonicalSpec JSON roundtrip with more complex conversation
// ---------------------------------------------------------------------------

func TestCanonicalSpec_JSON_Complex(t *testing.T) {
	now := time.Now().UTC()
	spec := &CanonicalSpec{
		ID:         "spec-complex-123",
		CreatedAt:  now,
		UserPrompt: "Build a REST API with Go",
		Objective:  "Implement a REST API for user management",
		InScope: []string{
			"User CRUD endpoints",
			"Authentication middleware",
			"JSON request/response",
			"Error handling",
		},
		OutOfScope: []string{
			"OAuth integration",
			"Database migrations",
			"Admin dashboard",
		},
		Acceptance: []string{
			"GET /users returns list of users",
			"POST /users creates a new user",
			"PUT /users/:id updates a user",
			"DELETE /users/:id removes a user",
		},
		Context: "Using Go 1.21 with standard library net/http",
		Conversation: []Message{
			{
				Role:    "user",
				Content: "I need a REST API for user management in Go",
			},
			{
				Role:    "assistant",
				Content: "I'll create a REST API with CRUD operations for users",
			},
			{
				Role:    "user",
				Content: "Make sure to include authentication middleware",
			},
			{
				Role:    "assistant",
				Content: "I'll add JWT-based authentication middleware",
			},
		},
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got CanonicalSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Verify top-level fields
	if got.ID != spec.ID {
		t.Errorf("ID: expected %q, got %q", spec.ID, got.ID)
	}
	if got.CreatedAt != spec.CreatedAt {
		t.Errorf("CreatedAt: expected %v, got %v", spec.CreatedAt, got.CreatedAt)
	}
	if got.UserPrompt != spec.UserPrompt {
		t.Errorf("UserPrompt: expected %q, got %q", spec.UserPrompt, got.UserPrompt)
	}
	if got.Objective != spec.Objective {
		t.Errorf("Objective: expected %q, got %q", spec.Objective, got.Objective)
	}
	if got.Context != spec.Context {
		t.Errorf("Context: expected %q, got %q", spec.Context, got.Context)
	}

	// Verify slice fields
	if len(got.InScope) != len(spec.InScope) {
		t.Errorf("InScope length: expected %d, got %d", len(spec.InScope), len(got.InScope))
	}
	if len(got.OutOfScope) != len(spec.OutOfScope) {
		t.Errorf("OutOfScope length: expected %d, got %d", len(spec.OutOfScope), len(got.OutOfScope))
	}
	if len(got.Acceptance) != len(spec.Acceptance) {
		t.Errorf("Acceptance length: expected %d, got %d", len(spec.Acceptance), len(got.Acceptance))
	}
	if len(got.Conversation) != len(spec.Conversation) {
		t.Errorf("Conversation length: expected %d, got %d", len(spec.Conversation), len(got.Conversation))
	}

	// Verify conversation messages
	for i := range spec.Conversation {
		if got.Conversation[i].Role != spec.Conversation[i].Role {
			t.Errorf("Conversation[%d].Role: expected %q, got %q", i, spec.Conversation[i].Role, got.Conversation[i].Role)
		}
		if got.Conversation[i].Content != spec.Conversation[i].Content {
			t.Errorf("Conversation[%d].Content: expected %q, got %q", i, spec.Conversation[i].Content, got.Conversation[i].Content)
		}
	}
}

// ---------------------------------------------------------------------------
// SpecExtractionResult JSON - Confidence boundary values
// ---------------------------------------------------------------------------

func TestSpecExtractionResult_JSON_ConfidenceValues(t *testing.T) {
	testCases := []struct {
		name string
		conf float64
	}{
		{"confidence_0", 0.0},
		{"confidence_1", 1.0},
		{"confidence_05", 0.5},
		{"confidence_095", 0.95},
		{"confidence_100", 1.0},
		{"confidence_125", 1.25}, // over 1 but valid float
	}

	spec := &CanonicalSpec{ID: "test", Objective: "test"}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := &SpecExtractionResult{
				Spec:       spec,
				Confidence: tc.conf,
				Reasoning:  "test reasoning",
			}

			data, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}

			var got SpecExtractionResult
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if got.Confidence != tc.conf {
				t.Errorf("Confidence: expected %f, got %f", tc.conf, got.Confidence)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// findLineNumberInDiff - additional edge cases
// ---------------------------------------------------------------------------

func TestFindLineNumberInDiff_EdgeCases(t *testing.T) {
	t.Run("file with slashes in name", func(t *testing.T) {
		diff := `diff --git a/pkg/auth/login.go b/pkg/auth/login.go
--- a/pkg/auth/login.go
+++ b/pkg/auth/login.go
@@ -1,3 +1,4 @@
 package auth
+func NewHandler() {}
 func Login() {}`
		line := findLineNumberInDiff(diff, "pkg/auth/login.go", "NewHandler")
		if line <= 0 {
			t.Errorf("expected positive line number for nested path, got %d", line)
		}
	})

	t.Run("description truncated if > 50 chars", func(t *testing.T) {
		longDesc := "This is a very long description that is over fifty characters long for testing purposes"
		diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+// This is a very long description that is over fifty characters long for testing purposes
 func main() {}`
		// Description > 50 chars is truncated to 50 chars, so we search for first 50 chars
		// The diff line starts with + so after + we get "// This is..."
		// truncated desc is "This is a very long description that is over fifty characters long for "
		// The diff line is "// This is a very long description that is over fifty..."
		// looseMatch will compare these and should find a match
		line := findLineNumberInDiff(diff, "main.go", longDesc)
		if line <= 0 {
			t.Errorf("expected positive line number for long description, got %d", line)
		}
	})

	t.Run("multiple files in diff - correct file selected", func(t *testing.T) {
		diff := `diff --git a/other.go b/other.go
--- a/other.go
+++ b/other.go
@@ -1,3 +1,3 @@
-func Old() {}
+func New() {}

diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
-func OldMain() {}
+func NewMain() {}`
		line := findLineNumberInDiff(diff, "main.go", "NewMain")
		if line <= 0 {
			t.Errorf("expected positive line number for main.go, got %d", line)
		}

		// Verify it doesn't find it in the other file
		line = findLineNumberInDiff(diff, "other.go", "NewMain")
		if line != 0 {
			t.Errorf("expected 0 for wrong file, got %d", line)
		}
	})
}

// ---------------------------------------------------------------------------
// looseMatch - additional edge cases
// ---------------------------------------------------------------------------

func TestLooseMatch_EdgeCases(t *testing.T) {
	t.Run("single long word", func(t *testing.T) {
		// Both words >= 3 chars, one contains the other
		if !looseMatch("authentication", "authentication service") {
			t.Error("should match when pattern word is contained in text word")
		}
	})

	t.Run("text with + prefix trimming", func(t *testing.T) {
		if !looseMatch("login", "+func Login()") {
			t.Error("should match after trimming + prefix")
		}
	})

	t.Run("numbers in words", func(t *testing.T) {
		if !looseMatch("func1", "func1 added") {
			t.Error("should match words containing numbers")
		}
	})

	t.Run("mixed case", func(t *testing.T) {
		if !looseMatch("HelloWorld", "hello world") {
			t.Error("should be case insensitive")
		}
	})
}

// ---------------------------------------------------------------------------
// JSON with special characters and unicode
// ---------------------------------------------------------------------------

func TestCanonicalSpec_JSON_SpecialChars(t *testing.T) {
	spec := &CanonicalSpec{
		ID:         "spec-special",
		Objective:  "Handle special chars: <>&\"' and unicode: ñ é ü 你好",
		InScope:    []string{"Handle \"quotes\"", "Unicode: café, naïve"},
		Acceptance: []string{"Status code: 200 OK", "No XSS: escape < >"},
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got CanonicalSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Objective != spec.Objective {
		t.Errorf("Objective with special chars mismatch:\nexpected: %q\ngot:      %q", spec.Objective, got.Objective)
	}
}

// ---------------------------------------------------------------------------
// ScopeViolation - type and severity combinations
// ---------------------------------------------------------------------------

func TestScopeViolation_JSON_TypeVariants(t *testing.T) {
	types := []string{"addition", "modification", "removal"}
	for _, typ := range types {
		t.Run("type="+typ, func(t *testing.T) {
			violation := ScopeViolation{
				File:        "test.go",
				Line:        1,
				Type:        typ,
				Severity:    "medium",
				Description: "test " + typ,
				Why:         "test violation",
			}

			data, err := json.Marshal(violation)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}

			var got ScopeViolation
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if got.Type != typ {
				t.Errorf("Type: expected %q, got %q", typ, got.Type)
			}
		})
	}
}
