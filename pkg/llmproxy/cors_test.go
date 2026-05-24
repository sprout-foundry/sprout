package llmproxy

import (
	"errors"
	"strings"
	"testing"
)

func TestIsCORSError_NilError(t *testing.T) {
	if IsCORSError(nil) {
		t.Error("nil error should not be detected as CORS")
	}
}

func TestIsCORSError_CORSIndicators(t *testing.T) {
	cases := []struct {
		name string
		err  string
	}{
		{
			name: "failed to fetch",
			err:  "Failed to fetch",
		},
		{
			name: "networkerror when attempting to fetch",
			err:  "NetworkError when attempting to fetch resource",
		},
		{
			name: "typeerror failed to fetch",
			err:  "TypeError: Failed to fetch",
		},
		{
			name: "load failed",
			err:  "Load failed",
		},
		{
			name: "blocked by cors policy",
			err:  "Access to fetch has been blocked by CORS policy",
		},
		{
			name: "cross-origin request blocked",
			err:  "cross-origin request blocked",
		},
		{
			name: "cors space-delimited",
			err:  "Error: CORS request rejected",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !IsCORSError(errors.New(c.err)) {
				t.Errorf("IsCORSError(%q) = false, want true", c.err)
			}
		})
	}
}

func TestIsCORSError_NonCORSErrors(t *testing.T) {
	cases := []struct {
		name string
		err  string
	}{
		{
			name: "connection refused",
			err:  "connection refused",
		},
		{
			name: "typeerror without fetch",
			err:  "TypeError: undefined is not a function",
		},
		{
			name: "fetch without typeerror",
			err:  "fetch request timed out",
		},
		{
			name: "generic server error",
			err:  "server returned 500 Internal Server Error",
		},
		{
			name: "authentication error",
			err:  "401 Unauthorized",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if IsCORSError(errors.New(c.err)) {
				t.Errorf("IsCORSError(%q) = true, want false", c.err)
			}
		})
	}
}

func TestIsCORSError_CaseInsensitivity(t *testing.T) {
	cases := []struct {
		name string
		err  string
	}{
		{
			name: "uppercase FAILED TO FETCH",
			err:  "FAILED TO FETCH",
		},
		{
			name: "uppercase NETWORKERROR",
			err:  "NETWORKERROR when attempting to fetch resource",
		},
		{
			name: "uppercase LOAD FAILED",
			err:  "LOAD FAILED",
		},
		{
			name: "mixed case TYPEERROR: Failed To Fetch",
			err:  "TYPEERROR: Failed To Fetch",
		},
		{
			name: "uppercase BLOCKED BY CORS POLICY",
			err:  "Access to fetch has been BLOCKED BY CORS POLICY",
		},
		{
			name: "uppercase CROSS-ORIGIN",
			err:  "CROSS-ORIGIN request blocked",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !IsCORSError(errors.New(c.err)) {
				t.Errorf("IsCORSError(%q) = false, want true (case-insensitive)", c.err)
			}
		})
	}
}

func TestCORSErrorMessage_ContainsExpectedContent(t *testing.T) {
	err := errors.New("Failed to fetch")
	msg := CORSErrorMessage(err, "openai")

	cases := []struct {
		desc     string
		contains string
	}{
		{"original error message", "Failed to fetch"},
		{"provider name", "openai"},
		{"CORS mention", "CORS"},
		{"setPlatformEndpoint mention", "setPlatformEndpoint"},
		{"setCorsProxy mention", "setCorsProxy"},
		{"browsers are indistinguishable", "indistinguishable"},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			if !strings.Contains(msg, c.contains) {
				t.Errorf("CORSErrorMessage should contain %q but got:\n%s", c.contains, msg)
			}
		})
	}
}

func TestCORSErrorMessage_DifferentProviders(t *testing.T) {
	cases := []struct {
		err      string
		provider string
	}{
		{"NetworkError when attempting to fetch resource", "anthropic"},
		{"Load failed", "openrouter"},
		{"Custom error message", "deepinfra"},
	}
	for _, c := range cases {
		t.Run(c.provider, func(t *testing.T) {
			msg := CORSErrorMessage(errors.New(c.err), c.provider)
			if !strings.Contains(msg, c.err) {
				t.Errorf("should include original error text %q", c.err)
			}
			if !strings.Contains(msg, c.provider) {
				t.Errorf("should include provider name %q", c.provider)
			}
		})
	}
}
