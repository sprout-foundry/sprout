//go:build !js

package cmd

import (
	"net/url"
	"runtime"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/repo"
)

func TestBuildBugReportURL(t *testing.T) {
	issueURL := buildBugReportURL()

	if !strings.HasPrefix(issueURL, repo.IssuesNewURL+"?") {
		t.Fatalf("URL %q does not start with the public new-issue endpoint %q", issueURL, repo.IssuesNewURL)
	}
	if strings.Contains(issueURL, "alantheprice") {
		t.Errorf("URL %q still points at the old repository", issueURL)
	}

	parsed, err := url.Parse(issueURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	q := parsed.Query()

	if got := q.Get("title"); got != bugReportTitle {
		t.Errorf("title = %q, want %q", got, bugReportTitle)
	}
	if got := q.Get("labels"); got != "bug" {
		t.Errorf("labels = %q, want %q", got, "bug")
	}

	body := q.Get("body")
	for _, want := range []string{
		"### What happened",
		"### What you expected",
		"### Steps to reproduce",
		"### Environment",
		"OS: " + runtime.GOOS,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}

	// The version must come from the build, not a literal.
	if !strings.Contains(body, "sprout version: "+bugVersion()) {
		t.Errorf("body does not carry the build version %q:\n%s", bugVersion(), body)
	}
}

func TestBuildBugReportURL_UsesBuildVersion(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v9.9.9"
	t.Cleanup(func() { buildinfo.Version = old })

	body, _ := url.Parse(buildBugReportURL())
	if got := body.Query().Get("body"); !strings.Contains(got, "sprout version: v9.9.9") {
		t.Errorf("body does not carry the injected version:\n%s", got)
	}
}

func TestBuildBugReportURL_NoSensitiveFields(t *testing.T) {
	body, _ := url.Parse(buildBugReportURL())
	decoded := strings.ToLower(body.Query().Get("body"))
	for _, needle := range []string{"/users/", "/home/", "workspace", "session", "sk-", "api_key", "token", "prompt"} {
		if strings.Contains(decoded, needle) {
			t.Errorf("body contains sensitive token %q:\n%s", needle, decoded)
		}
	}
}

func TestBuildBugReportURL_UnderLengthCap(t *testing.T) {
	if got := len(buildBugReportURL()); got > maxBugURLBytes {
		t.Errorf("URL length = %d, want <= %d", got, maxBugURLBytes)
	}
}

func TestBuildBugReportURLForBody_TruncatesUnderCap(t *testing.T) {
	// A body heavy in characters that percent-encode to 3 bytes (newlines →
	// %0A) exercises the truncation branch: trimming against the RAW length
	// would overshoot the cap, so the builder must trim against the encoded
	// length.
	for _, n := range []int{9000, 20000, 50000, 200000} {
		body := strings.Repeat("\n", n)
		got := buildBugReportURLForBody(body)
		if len(got) > maxBugURLBytes {
			t.Errorf("body of %d newlines: URL length = %d, want <= %d", n, len(got), maxBugURLBytes)
		}
		if !strings.HasPrefix(got, repo.IssuesNewURL+"?") {
			t.Errorf("truncated URL lost the endpoint: %q", got[:min(80, len(got))])
		}
		parsed, err := url.Parse(got)
		if err != nil {
			t.Fatalf("parse truncated URL: %v", err)
		}
		if parsed.Query().Get("labels") != "bug" {
			t.Errorf("truncated URL lost the bug label")
		}
		if !strings.Contains(parsed.Query().Get("body"), "…(truncated)") {
			t.Errorf("truncated body is missing the marker")
		}
	}
}

func TestBugCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"bug"})
	if err != nil {
		t.Fatalf("find bug command: %v", err)
	}
	if cmd.Name() != "bug" {
		t.Errorf("command name = %q, want bug", cmd.Name())
	}
}
