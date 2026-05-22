package security

import (
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/assert"
)

// realisticOpenAIKey matches gitleaks' openai-api-key rule shape
// (sk- + 20 alphanumeric + T3BlbkFJ + 20 alphanumeric). Not a live key.
const realisticOpenAIKey = "sk-AbCdEfGhIjKlMnOpQrStT3BlbkFJ1234567890abcdefghij"

// realisticJWT is a syntactically valid JWT (header.payload.signature, each
// segment base64url-encoded). Not a live token.
const realisticJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		filePath string
		expected bool
	}{
		{name: "test file path", content: "some code", filePath: "example_test.go", expected: true},
		{name: "test file path with _test suffix", content: "some code", filePath: "myfile_test.go", expected: true},
		{name: "example in path", content: "some code", filePath: "config_example.json", expected: true},
		{name: "demo in path", content: "some code", filePath: "demo.go", expected: true},
		{name: "sample in path", content: "some code", filePath: "sample_config.yaml", expected: true},
		{name: "mock in path", content: "some code", filePath: "mock_server.go", expected: true},
		{name: ".env.example file", content: "API_KEY=test", filePath: ".env.example", expected: true},
		{name: "config.example file", content: "{}", filePath: "config.example", expected: true},
		{name: "regular file path", content: "some code", filePath: "main.go", expected: false},
		{name: "# test comment", content: "# test comment", filePath: "main.go", expected: true},
		{name: "// test comment", content: "// test comment", filePath: "main.go", expected: true},
		{name: "/* test comment */", content: "/* test comment */", filePath: "main.go", expected: true},
		{name: "test_ function prefix", content: "func Test_foo()", filePath: "main.go", expected: true},
		{name: "_test function suffix", content: "func foo_test()", filePath: "main.go", expected: true},
		{name: "# example comment", content: "# example config", filePath: "main.go", expected: true},
		{name: "// demo comment", content: "// demo usage", filePath: "main.go", expected: true},
		{name: "# placeholder comment", content: "# placeholder value", filePath: "main.go", expected: true},
		{name: "# sample comment", content: "# sample code", filePath: "main.go", expected: true},
		{name: "# mock comment", content: "# mock data", filePath: "main.go", expected: true},
		// Note: PASS/FAIL/TODO/FIXME indicators are uppercase in the indicator
		// list but compared against lowercased content — a pre-existing quirk
		// that makes these markers ineffective. Preserving that behaviour here.
		{name: "PASS indicator", content: "PASS: test passed", filePath: "main.go", expected: false},
		{name: "FAIL indicator", content: "FAIL: test failed", filePath: "main.go", expected: false},
		{name: "TODO indicator", content: "TODO: implement this", filePath: "main.go", expected: false},
		{name: "FIXME indicator", content: "FIXME: fix this bug", filePath: "main.go", expected: false},
		{name: "regular content no test indicators", content: `func main() { fmt.Println("hello") }`, filePath: "main.go", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTestFile(tt.content, tt.filePath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectSecurityConcerns_RealisticOpenAIKey(t *testing.T) {
	concerns, snippets := DetectSecurityConcerns(`OPENAI_API_KEY="` + realisticOpenAIKey + `"`)
	assert.Contains(t, concerns, "OpenAI API Key Exposure")
	assert.NotEmpty(t, snippets["OpenAI API Key Exposure"])
}

func TestDetectSecurityConcerns_RealisticJWT(t *testing.T) {
	concerns, _ := DetectSecurityConcerns(`token: ` + realisticJWT)
	assert.Contains(t, concerns, "JWT Exposure")
}

func TestDetectSecurityConcerns_PlaceholdersNotFlagged(t *testing.T) {
	// All of these were false-positive sources for the legacy regex; gitleaks'
	// default config (keyword pre-filter + entropy + stopwords) should reject
	// them all.
	inputs := []string{
		`<input value="OPENAI_API_KEY=" placeholder="OPENAI_API_KEY">`,
		`OPENAI_API_KEY=PLACEHOLDER_TEXT_GOES_HERE`,
		`# OPENAI_API_KEY=your_key_here`,
		`api_key = "your-api-key-here"`,
		`api_key = "changeme"`,
		`Example: api_key=sk-...`,
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(in)
			assert.Empty(t, concerns, "expected no concerns for placeholder input")
		})
	}
}

func TestDetectSecurityConcerns_DataURLsNotFlagged(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "data URL with JWT-like base64 payload - SVG",
			content: "data:image/svg+xml;base64,eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
		},
		{
			name:    "data URL with PNG base64 payload",
			content: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk",
		},
		{
			name:    "data URL with minimal JWT-like payload",
			content: "data:application/json;base64,eyJhIjoiYiJ9.eyJjIjoiZCJ9.e30",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(tt.content)
			assert.Empty(t, concerns, "data URLs should be stripped before scanning")
		})
	}

	// Sanity check: a real JWT NOT inside a data URL should still be flagged.
	concerns, _ := DetectSecurityConcerns("token: " + realisticJWT)
	assert.Contains(t, concerns, "JWT Exposure", "JWTs outside data URLs should still be detected")
}

func TestDetectSecurityConcerns_DatabaseURLs(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{name: "MongoDB URL localhost", content: `mongodb://admin:password@localhost:27017/db`, expected: nil},
		{name: "PostgreSQL URL remote", content: `postgresql://user:pass@prod-db.example.com:5432/mydb`, expected: []string{"Database/Service Creds Exposure"}},
		{name: "MySQL URL remote", content: `mysql://user:pass@db.example.com:3306/mydb`, expected: []string{"Database/Service Creds Exposure"}},
		{name: "Redis URL remote", content: `redis://:password@redis.example.com:6379/0`, expected: []string{"Database/Service Creds Exposure"}},
		{name: "JDBC URL remote", content: `jdbc:postgresql://db.example.com:5432/mydb`, expected: []string{"Database/Service Creds Exposure"}},
		{name: "localhost filtered", content: `mongodb://localhost:27017/test`, expected: nil},
		{name: "127.0.0.1 filtered", content: `postgresql://user:pass@127.0.0.1:5432/test`, expected: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(tt.content)
			assert.Equal(t, tt.expected, concerns)
		})
	}
}

func TestDetectSecurityConcernsWithContext(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		filePath string
		assert   func(t *testing.T, concerns []string)
	}{
		{
			name:     "placeholder-shaped value in test file is filtered",
			content:  `api_key = "your-api-key-1234567890"`,
			filePath: "main_test.go",
			assert: func(t *testing.T, concerns []string) {
				assert.Empty(t, concerns)
			},
		},
		{
			name:     "realistic key in production file is detected",
			content:  `OPENAI_API_KEY="` + realisticOpenAIKey + `"`,
			filePath: "main.go",
			assert: func(t *testing.T, concerns []string) {
				assert.Contains(t, concerns, "OpenAI API Key Exposure")
			},
		},
		{
			name:     "remote DB URL detected outside test file",
			content:  `mongodb://admin:pass@mongo.example.com:27017`,
			filePath: "config.go",
			assert: func(t *testing.T, concerns []string) {
				assert.Contains(t, concerns, "Database/Service Creds Exposure")
			},
		},
		{
			name:     "local DB URL filtered everywhere",
			content:  `postgresql://user:pass@127.0.0.1:5432/db`,
			filePath: "config.go",
			assert: func(t *testing.T, concerns []string) {
				assert.Empty(t, concerns)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcernsWithContext(tt.content, tt.filePath)
			tt.assert(t, concerns)
		})
	}
}

func TestDetectSecurityConcerns_Snippets(t *testing.T) {
	content := `OPENAI_API_KEY="` + realisticOpenAIKey + `"`
	concerns, snippets := DetectSecurityConcerns(content)
	assert.Contains(t, concerns, "OpenAI API Key Exposure")
	assert.Contains(t, snippets["OpenAI API Key Exposure"], "T3BlbkFJ")
}

func TestDetectSecurityConcerns_SortedConcerns(t *testing.T) {
	content := `OPENAI_API_KEY="` + realisticOpenAIKey + `" and ` +
		`token: ` + realisticJWT + ` and mongodb://admin:pass@mongo.example.com:27017`
	concerns, _ := DetectSecurityConcerns(content)

	for i := 1; i < len(concerns); i++ {
		assert.LessOrEqual(t, concerns[i-1], concerns[i], "concerns must be sorted")
	}
	assert.Contains(t, concerns, "OpenAI API Key Exposure")
	assert.Contains(t, concerns, "JWT Exposure")
	assert.Contains(t, concerns, "Database/Service Creds Exposure")
}

func TestDetectSecurityConcerns_EmptyAndNoMatches(t *testing.T) {
	cases := []string{"", `func main() { fmt.Println("hello world") }`}
	for _, c := range cases {
		concerns, _ := DetectSecurityConcerns(c)
		assert.Empty(t, concerns)
	}
}

// --- SecurityPromptManager timeout tests ---

func TestSecurityPromptManager_Timeout(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewApprovalManager()

	// Set a very short timeout so the test doesn't wait 5 minutes
	mgr.SetPromptTimeout(50 * time.Millisecond)

	// Drain the published event so the EventBus doesn't block
	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	done := make(chan bool, 1)
	go func() {
		response := mgr.RequestPrompt(eb, "", "Allow this?", false, nil)
		done <- response
	}()

	// Consume the event but intentionally never respond,
	// so RequestPrompt must hit the timeout path.
	go func() {
		<-eventCh // drain the event, then do nothing
	}()

	select {
	case response := <-done:
		if response {
			t.Error("expected defaultResponse (false) when prompt times out (no response sent)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("test timed out — RequestPrompt did not return within 2s")
	}
}

func TestSecurityPromptManager_TimeoutWithDefaultTrue(t *testing.T) {
	// When defaultResponse is true, timeout should return true (the safe default)
	eb := events.NewEventBus()
	mgr := NewApprovalManager()

	mgr.SetPromptTimeout(50 * time.Millisecond)

	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	done := make(chan bool, 1)
	go func() {
		response := mgr.RequestPrompt(eb, "", "Allow this?", true, nil)
		done <- response
	}()

	// Drain event but don't respond
	go func() {
		<-eventCh
	}()

	select {
	case response := <-done:
		if !response {
			t.Error("expected defaultResponse (true) when prompt times out")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("test timed out")
	}
}

func TestSecurityPromptManager_SetPromptTimeout(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewApprovalManager()

	// Set a short custom timeout and verify it takes effect
	mgr.SetPromptTimeout(30 * time.Millisecond)
	eventCh := eb.Subscribe("timeout_test")
	defer eb.Unsubscribe("timeout_test")

	done := make(chan bool, 1)
	go func() {
		response := mgr.RequestPrompt(eb, "", "Continue?", false, nil)
		done <- response
	}()

	// Drain event but don't respond
	go func() {
		<-eventCh
	}()

	select {
	case response := <-done:
		if response {
			t.Error("expected false when custom short timeout expires")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("test timed out")
	}

	// Reset to default via zero value and verify request still works
	mgr.SetPromptTimeout(0)
	eventCh2 := eb.Subscribe("timeout_test2")
	defer eb.Unsubscribe("timeout_test2")

	go func() {
		event := <-eventCh2
		data, _ := event.Data.(map[string]interface{})
		requestID, _ := data["request_id"].(string)
		mgr.RespondToPrompt(requestID, true)
	}()

	response := mgr.RequestPrompt(eb, "", "Continue?", false, nil)
	if !response {
		t.Error("expected true after resetting timeout to default (response sent immediately)")
	}
}

func TestSecurityPromptManager_TimeoutDoesNotBlockIfResponseArrives(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewApprovalManager()

	// Set a long timeout (10 seconds) but respond immediately
	mgr.SetPromptTimeout(10 * time.Second)

	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	// Respond immediately upon receiving the event
	go func() {
		event := <-eventCh
		data, _ := event.Data.(map[string]interface{})
		requestID, _ := data["request_id"].(string)
		mgr.RespondToPrompt(requestID, true)
	}()

	start := time.Now()
	response := mgr.RequestPrompt(eb, "", "Allow this?", false, nil)
	elapsed := time.Since(start)

	if !response {
		t.Error("expected true when response arrives before timeout")
	}
	if elapsed > 2*time.Second {
		t.Errorf("RequestPrompt took too long (%v) — should have returned immediately on response", elapsed)
	}
}
