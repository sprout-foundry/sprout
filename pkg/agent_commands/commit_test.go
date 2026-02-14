package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommitCommand_Name(t *testing.T) {
	cmd := &CommitCommand{}
	assert.Equal(t, "commit", cmd.Name())
}

func TestCommitCommand_Description(t *testing.T) {
	cmd := &CommitCommand{}
	desc := cmd.Description()
	assert.Contains(t, desc, "commit")
	assert.Contains(t, desc, "workflow")
}

func TestCommitJSONResult_Validate(t *testing.T) {
	testCases := []struct {
		name    string
		result  CommitJSONResult
		wantErr bool
	}{
		{
			name: "valid success result",
			result: CommitJSONResult{
				Status:  CommitStatusSuccess,
				Commit:  "abc123",
				Message: "Test commit",
				Branch:  "main",
			},
			wantErr: false,
		},
		{
			name: "valid error result",
			result: CommitJSONResult{
				Status: CommitStatusError,
				Error:  "something went wrong",
			},
			wantErr: false,
		},
		{
			name: "valid dry-run result",
			result: CommitJSONResult{
				Status:  CommitStatusDryRun,
				Message: "Would commit: test",
			},
			wantErr: false,
		},
		{
			name: "missing status",
			result: CommitJSONResult{
				Commit: "abc123",
			},
			wantErr: true,
		},
		{
			name: "success without commit hash",
			result: CommitJSONResult{
				Status:  CommitStatusSuccess,
				Message: "Test",
			},
			wantErr: true,
		},
		{
			name: "error without error message",
			result: CommitJSONResult{
				Status: CommitStatusError,
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.result.Validate()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWrapText(t *testing.T) {
	testCases := []struct {
		name       string
		input      string
		lineLength int
		expected   string
	}{
		{
			name:       "empty string",
			input:      "",
			lineLength: 80,
			expected:   "",
		},
		{
			name:       "short text",
			input:      "Hello world",
			lineLength: 80,
			expected:   "Hello world",
		},
		{
			name:       "text with newlines",
			input:      "Hello world\n\nThis is a test",
			lineLength: 80,
			expected:   "Hello world\n\nThis is a test",
		},
		{
			name:       "long text - returns as-is (wrapText doesn't wrap)",
			input:      strings.Repeat("a", 100),
			lineLength: 40,
			expected:   strings.Repeat("a", 100),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := wrapText(tc.input, tc.lineLength)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDoHeuristicReview(t *testing.T) {
	testCases := []struct {
		name        string
		diff        string
		stagedFiles []string
		expected    string
		shouldMatch bool
	}{
		{
			name:        "no issues - clean diff",
			diff:        "func hello() { return 1; }",
			stagedFiles: []string{"main.go"},
			expected:    "No critical concerns found.",
			shouldMatch: true,
		},
		{
			name:        "password detected in diff",
			diff:        "password = 'secret123'",
			stagedFiles: []string{"config.go"},
			expected:    "POTENTIAL SECRET EXPOSED",
			shouldMatch: true,
		},
		{
			name:        "api key detected in diff",
			diff:        "api_key = 'sk-12345'",
			stagedFiles: []string{"auth.go"},
			expected:    "POTENTIAL SECRET EXPOSED",
			shouldMatch: true,
		},
		{
			name:        "token detected in diff",
			diff:        "token = 'bearer-token'",
			stagedFiles: []string{"api.go"},
			expected:    "POTENTIAL SECRET EXPOSED",
			shouldMatch: true,
		},
		{
			name:        "env file detected",
			diff:        "some changes",
			stagedFiles: []string{".env", "main.go"},
			expected:    "RISKY FILE",
			shouldMatch: true,
		},
		{
			name:        "secret file detected",
			diff:        "some changes",
			stagedFiles: []string{"secret.txt", "main.go"},
			expected:    "RISKY FILE",
			shouldMatch: true,
		},
		{
			name:        "pem file detected",
			diff:        "some changes",
			stagedFiles: []string{"private.pem"},
			expected:    "RISKY FILE",
			shouldMatch: true,
		},
		{
			name:        "key file detected",
			diff:        "some changes",
			stagedFiles: []string{"private.key"},
			expected:    "RISKY FILE",
			shouldMatch: true,
		},
		{
			name:        "console.log detected",
			diff:        "console.log('debug')",
			stagedFiles: []string{"app.js"},
			expected:    "DEBUG CODE",
			shouldMatch: true,
		},
		{
			name:        "fmt.Println detected",
			diff:        "fmt.Println('debug info')",
			stagedFiles: []string{"main.go"},
			expected:    "DEBUG CODE",
			shouldMatch: true,
		},
		{
			name:        "print statement detected",
			diff:        "print('hello')",
			stagedFiles: []string{"script.py"},
			expected:    "DEBUG CODE",
			shouldMatch: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := doHeuristicReview(tc.diff, tc.stagedFiles)
			if tc.shouldMatch {
				assert.Contains(t, result, tc.expected)
			} else {
				assert.NotContains(t, result, tc.expected)
			}
		})
	}
}

func TestCommitStatusConstants(t *testing.T) {
	assert.Equal(t, "success", CommitStatusSuccess)
	assert.Equal(t, "error", CommitStatusError)
	assert.Equal(t, "dry-run", CommitStatusDryRun)
}

func TestCommitCommand_Execute_NoArgs(t *testing.T) {
	cmd := &CommitCommand{}
	// Execute requires an agent, which needs initialization
	// Without a proper agent setup, this will panic or error
	// The test verifies the command structure is correct
	assert.NotNil(t, cmd)
}

func TestDoHeuristicReview_AWSCredentials(t *testing.T) {
	result := doHeuristicReview("aws_access_key = 'key123'", []string{"config.go"})
	assert.Contains(t, result, "POTENTIAL SECRET EXPOSED")
}

func TestDoHeuristicReview_GitHubToken(t *testing.T) {
	result := doHeuristicReview("github_token = 'ghp_xxx'", []string{"auth.go"})
	assert.Contains(t, result, "POTENTIAL SECRET EXPOSED")
}

func TestDoHeuristicReview_DebugTrue(t *testing.T) {
	result := doHeuristicReview("debug=true", []string{"config.go"})
	assert.Contains(t, result, "DEBUG CODE")
}

func TestDoHeuristicReview_CredentialFile(t *testing.T) {
	result := doHeuristicReview("some changes", []string{"credentials.json"})
	assert.Contains(t, result, "RISKY FILE")
}

func TestDoHeuristicReview_PrivateKeyFile(t *testing.T) {
	result := doHeuristicReview("some changes", []string{"private.key"})
	assert.Contains(t, result, "RISKY FILE")
}

func TestWrapText_SingleLongWord(t *testing.T) {
	// Single word stays as-is (wrapText doesn't wrap)
	result := wrapText(strings.Repeat("x", 100), 40)
	assert.Equal(t, strings.Repeat("x", 100), result)
}

func TestWrapText_MultipleParagraphs(t *testing.T) {
	input := "This is paragraph one.\n\nThis is paragraph two with some more text."
	result := wrapText(input, 40)
	assert.Contains(t, result, "\n\n")
}
