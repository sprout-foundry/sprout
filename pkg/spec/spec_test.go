package spec

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/history"
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
}

// ---------------------------------------------------------------------------
// ScopeReviewResult JSON Roundtrip
// ---------------------------------------------------------------------------

func TestScopeReviewResult_JSON(t *testing.T) {
	result := ScopeReviewResult{
		InScope: false,
		Violations: []ScopeViolation{
			{
				File:        "auth/login.go",
				Line:        42,
				Type:        "addition",
				Severity:    "high",
				Description: "OAuth login function added",
				Why:         "OAuth was explicitly excluded from spec",
			},
		},
		Summary: "Found 1 scope violation",
		Suggestions: []string{
			"Remove OAuthLogin function as OAuth was not requested",
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ScopeReviewResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.InScope != result.InScope {
		t.Errorf("InScope: expected %v, got %v", result.InScope, got.InScope)
	}
	if len(got.Violations) != len(result.Violations) {
		t.Fatalf("Violations: expected %d, got %d", len(result.Violations), len(got.Violations))
	}
	if got.Summary != result.Summary {
		t.Errorf("Summary: expected %q, got %q", result.Summary, got.Summary)
	}
	if len(got.Suggestions) != len(result.Suggestions) {
		t.Errorf("Suggestions: expected %d, got %d", len(result.Suggestions), len(got.Suggestions))
	}
}

func TestScopeReviewResult_JSON_InScope(t *testing.T) {
	result := ScopeReviewResult{
		InScope:     true,
		Violations:  nil,
		Summary:     "All changes are within scope",
		Suggestions: nil,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ScopeReviewResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if !got.InScope {
		t.Error("expected InScope to be true")
	}
	if len(got.Violations) != 0 {
		t.Errorf("expected no violations, got %d", len(got.Violations))
	}
}

func TestScopeReviewResult_JSON_Empty(t *testing.T) {
	result := ScopeReviewResult{}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ScopeReviewResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.InScope {
		t.Error("expected InScope to be false")
	}
	if len(got.Violations) != 0 {
		t.Errorf("expected no violations, got %d", len(got.Violations))
	}
}

// ---------------------------------------------------------------------------
// ChangeReviewResult JSON Roundtrip
// ---------------------------------------------------------------------------

func TestChangeReviewResult_JSON(t *testing.T) {
	spec := &CanonicalSpec{
		ID:         "spec-review-1",
		CreatedAt:  time.Now(),
		UserPrompt: "Review changes",
		Objective:  "Review tracked changes",
	}
	specResult := &SpecExtractionResult{
		Spec:       spec,
		Confidence: 0.9,
		Reasoning:  "Review spec extracted",
	}
	scopeResult := &ScopeReviewResult{
		InScope:     true,
		Summary:     "All changes within scope",
		Violations:  nil,
		Suggestions: nil,
	}

	result := ChangeReviewResult{
		SpecResult:   specResult,
		ScopeResult:  scopeResult,
		FilesChanged: 3,
		TotalChanges: 5,
		RevisionID:   "rev-abc-123",
		Summary:      "Reviewed 3 files with 5 changes",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ChangeReviewResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.RevisionID != result.RevisionID {
		t.Errorf("RevisionID: expected %q, got %q", result.RevisionID, got.RevisionID)
	}
	if got.FilesChanged != result.FilesChanged {
		t.Errorf("FilesChanged: expected %d, got %d", result.FilesChanged, got.FilesChanged)
	}
	if got.TotalChanges != result.TotalChanges {
		t.Errorf("TotalChanges: expected %d, got %d", result.TotalChanges, got.TotalChanges)
	}
	if got.Summary != result.Summary {
		t.Errorf("Summary: expected %q, got %q", result.Summary, got.Summary)
	}
	if got.SpecResult == nil || got.SpecResult.Spec == nil {
		t.Fatal("expected non-nil SpecResult.Spec")
	}
	if got.ScopeResult == nil {
		t.Fatal("expected non-nil ScopeResult")
	}
}

func TestChangeReviewResult_JSON_ZeroValues(t *testing.T) {
	result := ChangeReviewResult{
		RevisionID:   "rev-empty",
		FilesChanged: 0,
		TotalChanges: 0,
		Summary:      "No changes to review",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ChangeReviewResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.RevisionID != result.RevisionID {
		t.Errorf("RevisionID: expected %q, got %q", result.RevisionID, got.RevisionID)
	}
	if got.FilesChanged != 0 {
		t.Errorf("expected FilesChanged=0, got %d", got.FilesChanged)
	}
	if got.TotalChanges != 0 {
		t.Errorf("expected TotalChanges=0, got %d", got.TotalChanges)
	}
	if got.Summary != result.Summary {
		t.Errorf("Summary: expected %q, got %q", result.Summary, got.Summary)
	}
}

// ---------------------------------------------------------------------------
// findLineNumberInDiff
// ---------------------------------------------------------------------------

func TestFindLineNumberInDiff(t *testing.T) {
	t.Run("finds line in single hunk", func(t *testing.T) {
		diff := `diff --git a/auth/login.go b/auth/login.go
--- a/auth/login.go
+++ b/auth/login.go
@@ -10,3 +10,4 @@ func Login() {
 	if !isValid {
+		log.Printf("Invalid login attempt")
 		return
 	}
 }`
		line := findLineNumberInDiff(diff, "auth/login.go", "Invalid login attempt")
		if line <= 0 {
			t.Errorf("expected positive line number, got %d", line)
		}
	})

	t.Run("finds line with exact description match", func(t *testing.T) {
		diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,5 +1,6 @@
 package main
+// New comment added
 func main() {`
		line := findLineNumberInDiff(diff, "main.go", "New comment added")
		if line <= 0 {
			t.Errorf("expected positive line number, got %d", line)
		}
	})

	t.Run("returns 0 when file not found", func(t *testing.T) {
		diff := `diff --git a/other.go b/other.go
--- a/other.go
+++ b/other.go
@@ -1,3 +1,3 @@
-func Old() {}
+func New() {}`
		line := findLineNumberInDiff(diff, "nonexistent.go", "test")
		if line != 0 {
			t.Errorf("expected 0, got %d", line)
		}
	})

	t.Run("returns 0 when description not found", func(t *testing.T) {
		diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
-func Old() {}
+func New() {}`
		line := findLineNumberInDiff(diff, "main.go", "some totally unrelated description xyz")
		if line != 0 {
			t.Errorf("expected 0, got %d", line)
		}
	})

	t.Run("handles multiple hunks", func(t *testing.T) {
		diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
-func First() {}
+func FirstNew() {}
@@ -10,3 +10,4 @@
-func Second() {}
+func SecondNew() {}
+// Added here`
		// Should find the added line in either hunk
		line := findLineNumberInDiff(diff, "main.go", "Added here")
		if line <= 0 {
			t.Errorf("expected positive line number, got %d", line)
		}
	})

	t.Run("skips added lines with ++ prefix", func(t *testing.T) {
		diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
 func main() {
+++ this is a line starting with +++
+this is a real added line`
		// Should not match the ++ line, may or may not match the + line
		line := findLineNumberInDiff(diff, "main.go", "real added line")
		if line <= 0 {
			t.Errorf("expected positive line number, got %d", line)
		}
	})

	t.Run("empty diff returns 0", func(t *testing.T) {
		line := findLineNumberInDiff("", "main.go", "test")
		if line != 0 {
			t.Errorf("expected 0, got %d", line)
		}
	})
}

// ---------------------------------------------------------------------------
// looseMatch
// ---------------------------------------------------------------------------

func TestLooseMatch(t *testing.T) {
	t.Run("matching words return true", func(t *testing.T) {
		tests := []struct {
			name    string
			pattern string
			text    string
			want    bool
		}{
			{"exact match", "hello world", "hello world", true},
			{"partial match: word in text", "hello", "hello world", true},
			{"partial match: word contains pattern", "worldhello", "hello world", true},
			{"case insensitive", "Hello", "HELLO WORLD", true},
			{"different order", "world hello", "hello world", true},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got := looseMatch(tc.pattern, tc.text)
				if got != tc.want {
					t.Errorf("looseMatch(%q, %q) = %v, want %v", tc.pattern, tc.text, got, tc.want)
				}
			})
		}
	})

	t.Run("no match returns false", func(t *testing.T) {
		tests := []struct {
			name    string
			pattern string
			text    string
			want    bool
		}{
			{"completely different", "login", "register", false},
			{"no common words", "authentication", "authorization", false},
			{"pattern is substring of word but too short", "x", "xyz", false}, // x is < 3 chars
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got := looseMatch(tc.pattern, tc.text)
				if got != tc.want {
					t.Errorf("looseMatch(%q, %q) = %v, want %v", tc.pattern, tc.text, got, tc.want)
				}
			})
		}
	})

	t.Run("empty inputs return false", func(t *testing.T) {
		if looseMatch("", "something") {
			t.Error("empty pattern should return false")
		}
		if looseMatch("something", "") {
			t.Error("empty text should return false")
		}
		if looseMatch("", "") {
			t.Error("both empty should return false")
		}
	})

	t.Run("short words are skipped", func(t *testing.T) {
		// All words < 3 chars should be skipped, resulting in no match
		if looseMatch("a bb cc", "dd ee ff") {
			t.Error("short words should be skipped and return false")
		}
	})

	t.Run("handles + prefix in text", func(t *testing.T) {
		// Text with + prefix (diff line) should have prefix trimmed
		if !looseMatch("login", "+func Login()") {
			t.Error("should match after trimming + prefix")
		}
		if !looseMatch("auth", "+++ func Authenticate()") {
			t.Error("should handle ++ prefix trimming")
		}
	})

	t.Run("multiple significant words", func(t *testing.T) {
		// "OAuth" (5 chars) should match "OAuth was added"
		if !looseMatch("OAuth function", "OAuth function was added") {
			t.Error("should match significant words")
		}
	})
}

// ---------------------------------------------------------------------------
// changesToDiff
// ---------------------------------------------------------------------------

func TestChangesToDiff(t *testing.T) {
	t.Run("empty changes returns empty string", func(t *testing.T) {
		revision := &history.RevisionGroup{
			RevisionID: "rev-empty",
			Changes:    []history.ChangeLog{},
		}
		diff, err := changesToDiff(revision)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if diff != "" {
			t.Errorf("expected empty diff, got %q", diff)
		}
	})

	t.Run("nil changes returns empty string", func(t *testing.T) {
		revision := &history.RevisionGroup{
			RevisionID: "rev-nil",
			Changes:    nil,
		}
		diff, err := changesToDiff(revision)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if diff != "" {
			t.Errorf("expected empty diff, got %q", diff)
		}
	})

	t.Run("single change produces valid diff", func(t *testing.T) {
		revision := &history.RevisionGroup{
			RevisionID: "rev-single",
			Changes: []history.ChangeLog{
				{
					RequestHash:     "rev-single",
					Filename:        "main.go",
					OriginalCode:    "package main\n\nfunc main() {}",
					NewCode:         "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}",
					Description:     "Add hello print",
					Status:          "active",
					Timestamp:       time.Now(),
					HasConversation: false,
				},
			},
		}
		diff, err := changesToDiff(revision)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if diff == "" {
			t.Fatal("expected non-empty diff")
		}
		if !strings.Contains(diff, "diff --git a/main.go b/main.go") {
			t.Error("diff should contain header for main.go")
		}
		if !strings.Contains(diff, "--- a/main.go") {
			t.Error("diff should contain --- header")
		}
		if !strings.Contains(diff, "+++ b/main.go") {
			t.Error("diff should contain +++ header")
		}
		if !strings.Contains(diff, "@@") {
			t.Error("diff should contain hunk header")
		}
		if !strings.Contains(diff, "+\tfmt.Println") {
			t.Error("diff should contain added line with fmt.Println")
		}
		if !strings.Contains(diff, "-func main() {}") {
			t.Error("diff should contain removed line")
		}
	})

	t.Run("single change with identical content", func(t *testing.T) {
		revision := &history.RevisionGroup{
			RevisionID: "rev-identical",
			Changes: []history.ChangeLog{
				{
					Filename:        "main.go",
					OriginalCode:    "package main",
					NewCode:         "package main",
					Description:     "no change",
					Status:          "active",
					Timestamp:       time.Now(),
					HasConversation: false,
				},
			},
		}
		diff, err := changesToDiff(revision)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should produce a diff but with no actual changes shown (just context)
		// It will have the headers but no + or - lines
		if diff == "" {
			t.Fatal("expected non-empty diff for single file (even if identical)")
		}
		if strings.Contains(diff, "+package main") {
			t.Error("diff should not contain added line for identical content")
		}
	})

	t.Run("reverted changes are skipped", func(t *testing.T) {
		revision := &history.RevisionGroup{
			RevisionID: "rev-mixed",
			Changes: []history.ChangeLog{
				{
					Filename:        "active.go",
					OriginalCode:    "old",
					NewCode:         "new",
					Description:     "active change",
					Status:          "active",
					Timestamp:       time.Now(),
					HasConversation: false,
				},
				{
					Filename:        "reverted.go",
					OriginalCode:    "old2",
					NewCode:         "new2",
					Description:     "reverted change",
					Status:          "reverted",
					Timestamp:       time.Now(),
					HasConversation: false,
				},
			},
		}
		diff, err := changesToDiff(revision)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if diff == "" {
			t.Fatal("expected non-empty diff (active change exists)")
		}
		if strings.Contains(diff, "reverted.go") {
			t.Error("diff should not contain reverted.go")
		}
		if !strings.Contains(diff, "active.go") {
			t.Error("diff should contain active.go")
		}
	})

	t.Run("multiple changes produce multiple file diffs", func(t *testing.T) {
		revision := &history.RevisionGroup{
			RevisionID: "rev-multi",
			Changes: []history.ChangeLog{
				{
					Filename:        "file1.go",
					OriginalCode:    "old1",
					NewCode:         "new1",
					Status:          "active",
					Timestamp:       time.Now(),
					HasConversation: false,
				},
				{
					Filename:        "file2.go",
					OriginalCode:    "old2",
					NewCode:         "new2",
					Status:          "active",
					Timestamp:       time.Now(),
					HasConversation: false,
				},
			},
		}
		diff, err := changesToDiff(revision)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(diff, "file1.go") {
			t.Error("diff should contain file1.go")
		}
		if !strings.Contains(diff, "file2.go") {
			t.Error("diff should contain file2.go")
		}
		// Count file headers
		file1Count := strings.Count(diff, "diff --git a/file1.go")
		file2Count := strings.Count(diff, "diff --git a/file2.go")
		if file1Count != 1 {
			t.Errorf("expected 1 file1.go diff, got %d", file1Count)
		}
		if file2Count != 1 {
			t.Errorf("expected 1 file2.go diff, got %d", file2Count)
		}
	})

	t.Run("multi-line diff with common prefix", func(t *testing.T) {
		original := `package main

func main() {
	fmt.Println("hello")
}
`
		newCode := `package main

func main() {
	fmt.Println("hello")
	fmt.Println("world")
}
`
		revision := &history.RevisionGroup{
			RevisionID: "rev-prefix",
			Changes: []history.ChangeLog{
				{
					Filename:        "main.go",
					OriginalCode:    original,
					NewCode:         newCode,
					Status:          "active",
					Timestamp:       time.Now(),
					HasConversation: false,
				},
			},
		}
		diff, err := changesToDiff(revision)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have context lines from the common prefix
		if !strings.Contains(diff, "fmt.Println") {
			t.Error("diff should contain common context line")
		}
		if !strings.Contains(diff, "+\tfmt.Println(\"world\")") {
			t.Error("diff should contain the added line")
		}
	})

	t.Run("new file (empty original)", func(t *testing.T) {
		revision := &history.RevisionGroup{
			RevisionID: "rev-newfile",
			Changes: []history.ChangeLog{
				{
					Filename:        "newfile.go",
					OriginalCode:    "",
					NewCode:         "package newfile\n\nfunc NewFunc() {}",
					Status:          "active",
					Timestamp:       time.Now(),
					HasConversation: false,
				},
			},
		}
		diff, err := changesToDiff(revision)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if diff == "" {
			t.Fatal("expected non-empty diff for new file")
		}
		if !strings.Contains(diff, "diff --git a/newfile.go b/newfile.go") {
			t.Error("diff should contain header for newfile.go")
		}
		if !strings.Contains(diff, "+package newfile") {
			t.Error("diff should contain added line")
		}
	})
}

// ---------------------------------------------------------------------------
// buildConversationFromRevision
// ---------------------------------------------------------------------------

func TestBuildConversationFromRevision(t *testing.T) {
	t.Run("uses conversation from revision when available", func(t *testing.T) {
		revision := &history.RevisionGroup{
			RevisionID:   "rev-with-conv",
			Instructions: "Test instructions",
			Response:     "Test response",
			Conversation: []history.APIMessage{
				{Role: "user", Content: "First message"},
				{Role: "assistant", Content: "First response"},
				{Role: "user", Content: "Second message"},
			},
		}
		msgs := buildConversationFromRevision(revision)

		if len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(msgs))
		}
		if msgs[0].Role != "user" || msgs[0].Content != "First message" {
			t.Errorf("message 0: expected {user, First message}, got {%s, %s}", msgs[0].Role, msgs[0].Content)
		}
		if msgs[1].Role != "assistant" || msgs[1].Content != "First response" {
			t.Errorf("message 1: expected {assistant, First response}, got {%s, %s}", msgs[1].Role, msgs[1].Content)
		}
		if msgs[2].Role != "user" || msgs[2].Content != "Second message" {
			t.Errorf("message 2: expected {user, Second message}, got {%s, %s}", msgs[2].Role, msgs[2].Content)
		}
	})

	t.Run("falls back to instructions/response when no conversation", func(t *testing.T) {
		revision := &history.RevisionGroup{
			RevisionID:   "rev-no-conv",
			Instructions: "Implement login",
			Response:     "I will create a login form",
			Conversation: nil,
		}
		msgs := buildConversationFromRevision(revision)

		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
		if msgs[0].Role != "user" || msgs[0].Content != "Implement login" {
			t.Errorf("message 0: expected {user, Implement login}, got {%s, %s}", msgs[0].Role, msgs[0].Content)
		}
		if msgs[1].Role != "assistant" || msgs[1].Content != "I will create a login form" {
			t.Errorf("message 1: expected {assistant, I will create a login form}, got {%s, %s}", msgs[1].Role, msgs[1].Content)
		}
	})

	t.Run("empty conversation falls back to instructions/response", func(t *testing.T) {
		revision := &history.RevisionGroup{
			RevisionID:   "rev-empty-conv",
			Instructions: "Do something",
			Response:     "Done",
			Conversation: []history.APIMessage{},
		}
		msgs := buildConversationFromRevision(revision)

		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
		if msgs[0].Content != "Do something" {
			t.Errorf("message 0: expected Do something, got %s", msgs[0].Content)
		}
	})

	t.Run("preserves reasoning content from APIMessage", func(t *testing.T) {
		revision := &history.RevisionGroup{
			Conversation: []history.APIMessage{
				{
					Role:             "assistant",
					Content:          "Hello",
					ReasoningContent: "Reasoning here",
				},
			},
		}
		msgs := buildConversationFromRevision(revision)

		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		// APIMessage fields beyond Role/Content are lost in the conversion to spec.Message
		if msgs[0].Role != "assistant" {
			t.Errorf("expected role assistant, got %s", msgs[0].Role)
		}
		if msgs[0].Content != "Hello" {
			t.Errorf("expected content Hello, got %s", msgs[0].Content)
		}
	})
}

// ---------------------------------------------------------------------------
// buildReviewSummary
// ---------------------------------------------------------------------------

func TestBuildReviewSummary(t *testing.T) {
	specResult := &SpecExtractionResult{
		Confidence: 0.85,
		Reasoning:  "Spec extracted from conversation",
	}

	t.Run("in-scope summary", func(t *testing.T) {
		scopeResult := &ScopeReviewResult{
			InScope:     true,
			Summary:     "All changes are within scope",
			Violations:  nil,
			Suggestions: nil,
		}

		summary := buildReviewSummary(specResult, scopeResult, 3, 5)

		if !strings.Contains(summary, "Reviewed 3 file") {
			t.Error("summary should contain file count")
		}
		if !strings.Contains(summary, "5 change") {
			t.Error("summary should contain change count")
		}
		if !strings.Contains(summary, "85%") {
			t.Error("summary should contain confidence percentage")
		}
		if !strings.Contains(summary, "within scope") {
			t.Error("summary should mention being within scope")
		}
	})

	t.Run("out-of-scope summary with violations", func(t *testing.T) {
		scopeResult := &ScopeReviewResult{
			InScope: false,
			Violations: []ScopeViolation{
				{File: "a.go", Line: 1, Description: "Added OAuth"},
				{File: "b.go", Line: 2, Description: "Added social login"},
			},
			Summary:     "Found 2 scope violations",
			Suggestions: []string{"Remove OAuth", "Remove social login"},
		}

		summary := buildReviewSummary(specResult, scopeResult, 2, 4)

		if !strings.Contains(summary, "2 file") {
			t.Error("summary should contain 2 file count")
		}
		if !strings.Contains(summary, "4 change") {
			t.Error("summary should contain 4 change count")
		}
		if !strings.Contains(summary, "violation") {
			t.Error("summary should mention violations")
		}
	})

	t.Run("zero changes", func(t *testing.T) {
		scopeResult := &ScopeReviewResult{
			InScope: true,
		}
		summary := buildReviewSummary(specResult, scopeResult, 0, 0)

		if !strings.Contains(summary, "0 file") {
			t.Error("summary should contain 0 file count")
		}
		if !strings.Contains(summary, "0 change") {
			t.Error("summary should contain 0 change count")
		}
	})

	t.Run("confidence percentage formatting", func(t *testing.T) {
		specResult := &SpecExtractionResult{
			Confidence: 0.95,
		}
		scopeResult := &ScopeReviewResult{
			InScope: true,
		}
		summary := buildReviewSummary(specResult, scopeResult, 1, 1)

		// 0.95 * 100 = 95
		if !strings.Contains(summary, "95%") {
			t.Errorf("summary should contain 95%%, got: %s", summary)
		}
	})

	t.Run("low confidence", func(t *testing.T) {
		specResult := &SpecExtractionResult{
			Confidence: 0.3,
		}
		scopeResult := &ScopeReviewResult{
			InScope: true,
		}
		summary := buildReviewSummary(specResult, scopeResult, 1, 1)

		if !strings.Contains(summary, "30%") {
			t.Errorf("summary should contain 30%%, got: %s", summary)
		}
	})

	t.Run("exact zero confidence", func(t *testing.T) {
		specResult := &SpecExtractionResult{
			Confidence: 0.0,
		}
		scopeResult := &ScopeReviewResult{
			InScope: true,
		}
		summary := buildReviewSummary(specResult, scopeResult, 1, 1)

		if !strings.Contains(summary, "0%") {
			t.Errorf("summary should contain 0%%, got: %s", summary)
		}
	})
}

// ---------------------------------------------------------------------------
// countTotalChanges
// ---------------------------------------------------------------------------

func TestCountTotalChanges(t *testing.T) {
	t.Run("counts only active changes", func(t *testing.T) {
		revision := &history.RevisionGroup{
			Changes: []history.ChangeLog{
				{Status: "active"},
				{Status: "active"},
				{Status: "reverted"},
				{Status: "active"},
				{Status: "restored"},
			},
		}
		count := countTotalChanges(revision)
		if count != 3 {
			t.Errorf("expected 3 active changes, got %d", count)
		}
	})

	t.Run("empty changes returns 0", func(t *testing.T) {
		revision := &history.RevisionGroup{
			Changes: []history.ChangeLog{},
		}
		count := countTotalChanges(revision)
		if count != 0 {
			t.Errorf("expected 0, got %d", count)
		}
	})

	t.Run("nil changes returns 0", func(t *testing.T) {
		revision := &history.RevisionGroup{
			Changes: nil,
		}
		count := countTotalChanges(revision)
		if count != 0 {
			t.Errorf("expected 0, got %d", count)
		}
	})

	t.Run("all reverted returns 0", func(t *testing.T) {
		revision := &history.RevisionGroup{
			Changes: []history.ChangeLog{
				{Status: "reverted"},
				{Status: "reverted"},
			},
		}
		count := countTotalChanges(revision)
		if count != 0 {
			t.Errorf("expected 0, got %d", count)
		}
	})

	t.Run("all active", func(t *testing.T) {
		revision := &history.RevisionGroup{
			Changes: []history.ChangeLog{
				{Status: "active"},
				{Status: "active"},
				{Status: "active"},
				{Status: "active"},
			},
		}
		count := countTotalChanges(revision)
		if count != 4 {
			t.Errorf("expected 4, got %d", count)
		}
	})
}

// ---------------------------------------------------------------------------
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

// ---------------------------------------------------------------------------
// ScopeReviewResult - validation_unavailable scenario
// ---------------------------------------------------------------------------

func TestScopeReviewResult_JSON_ValidationUnavailable(t *testing.T) {
	result := ScopeReviewResult{
		InScope: false,
		Violations: []ScopeViolation{
			{
				File:        "<validation>",
				Line:        0,
				Type:        "validation_unavailable",
				Severity:    "high",
				Description: "Scope validation could not run because the provider was rate limited",
				Why:         "Cannot verify scope compliance without a successful validation pass",
			},
		},
		Summary:     "Scope validation unavailable due to rate limiting",
		Suggestions: []string{"Retry self-review after rate limits reset", "Reduce diff size before retrying"},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got ScopeReviewResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.InScope {
		t.Error("expected InScope to be false for validation unavailable")
	}
	if len(got.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(got.Violations))
	}
	if got.Violations[0].Type != "validation_unavailable" {
		t.Errorf("expected type validation_unavailable, got %q", got.Violations[0].Type)
	}
}

// ---------------------------------------------------------------------------
// changeIntegration: changesToDiff with multi-line original and new code
// ---------------------------------------------------------------------------

func TestChangesToDiff_MultilineCode(t *testing.T) {
	original := `package main

import "fmt"

func main() {
	fmt.Println("hello")
	fmt.Println("world")
}
`
	newCode := `package main

import (
	"fmt"
	"log"
)

func main() {
	fmt.Println("hello")
	fmt.Println("world")
	fmt.Println("goodbye")
	log.Println("done")
}
`

	revision := &history.RevisionGroup{
		RevisionID: "rev-multiline",
		Changes: []history.ChangeLog{
			{
				Filename:        "main.go",
				OriginalCode:    original,
				NewCode:         newCode,
				Status:          "active",
				Timestamp:       time.Now(),
				HasConversation: false,
			},
		},
	}

	diff, err := changesToDiff(revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain headers
	if !strings.Contains(diff, "diff --git a/main.go b/main.go") {
		t.Error("missing diff header")
	}
	if !strings.Contains(diff, "+++ b/main.go") {
		t.Error("missing +++ header")
	}

	// Should contain the added import "log"
	if !strings.Contains(diff, "log") {
		t.Error("diff should mention log import")
	}

	// Should contain added lines
	addCount := strings.Count(diff, "+\t")
	if addCount == 0 {
		t.Error("diff should contain at least one added line")
	}

	// Should contain removed lines
	remCount := strings.Count(diff, "-\t")
	if remCount == 0 {
		t.Error("diff should contain at least one removed line")
	}
}

// ---------------------------------------------------------------------------
// buildConversationFromRevision with APIToolCall data
// ---------------------------------------------------------------------------

func TestBuildConversationFromRevision_ToolCalls(t *testing.T) {
	revision := &history.RevisionGroup{
		Conversation: []history.APIMessage{
			{
				Role:    "assistant",
				Content: "Let me run a command",
				ToolCalls: []history.APIToolCall{
					{
						ID:   "call-123",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "execute_command", Arguments: "echo hello"},
					},
				},
			},
		},
	}

	msgs := buildConversationFromRevision(revision)

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("expected role assistant, got %s", msgs[0].Role)
	}
	if msgs[0].Content != "Let me run a command" {
		t.Errorf("expected content 'Let me run a command', got %s", msgs[0].Content)
	}
	// Note: ToolCall data is not preserved in spec.Message conversion
}

// ---------------------------------------------------------------------------
// JSON roundtrip for ChangeReviewResult with all populated fields
// ---------------------------------------------------------------------------

func TestChangeReviewResult_JSON_FullRoundtrip(t *testing.T) {
	spec := &CanonicalSpec{
		ID:           "spec-full",
		CreatedAt:    time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC),
		UserPrompt:   "Full test",
		Objective:    "Full test objective",
		InScope:      []string{"item1", "item2"},
		OutOfScope:   []string{"excluded"},
		Acceptance:   []string{"criterion1"},
		Context:      "test context",
		Conversation: []Message{{Role: "user", Content: "test"}},
	}

	specResult := &SpecExtractionResult{
		Spec:       spec,
		Confidence: 0.85,
		Reasoning:  "Extracted correctly",
	}

	scopeResult := &ScopeReviewResult{
		InScope: true,
		Violations: []ScopeViolation{
			{
				File:        "test.go",
				Line:        42,
				Type:        "addition",
				Severity:    "low",
				Description: "Minor change",
				Why:         "Out of scope",
			},
		},
		Summary:     "Review complete",
		Suggestions: []string{"Follow up later"},
	}

	result := ChangeReviewResult{
		SpecResult:   specResult,
		ScopeResult:  scopeResult,
		FilesChanged: 2,
		TotalChanges: 4,
		RevisionID:   "rev-full-123",
		Summary:      "Full review result",
	}

	// Marshal
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Unmarshal
	var got ChangeReviewResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	// Verify all fields
	if got.RevisionID != result.RevisionID {
		t.Errorf("RevisionID: expected %q, got %q", result.RevisionID, got.RevisionID)
	}
	if got.FilesChanged != result.FilesChanged {
		t.Errorf("FilesChanged: expected %d, got %d", result.FilesChanged, got.FilesChanged)
	}
	if got.TotalChanges != result.TotalChanges {
		t.Errorf("TotalChanges: expected %d, got %d", result.TotalChanges, got.TotalChanges)
	}
	if got.Summary != result.Summary {
		t.Errorf("Summary: expected %q, got %q", result.Summary, got.Summary)
	}
	if got.SpecResult.Confidence != result.SpecResult.Confidence {
		t.Errorf("SpecResult.Confidence: expected %f, got %f", result.SpecResult.Confidence, got.SpecResult.Confidence)
	}
	if got.ScopeResult.InScope != result.ScopeResult.InScope {
		t.Errorf("ScopeResult.InScope: expected %v, got %v", result.ScopeResult.InScope, got.ScopeResult.InScope)
	}
}
