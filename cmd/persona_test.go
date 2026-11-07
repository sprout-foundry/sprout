//go:build !js

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPersonaValidate_AcceptsAValidFile pins the happy path: a well-formed
// definition validates and is named.
func TestPersonaValidate_AcceptsAValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "foundry.json")
	body := `{"personas":[{"id":"foundry_planner","name":"Foundry Planner","description":"Plans platform work","system_prompt_text":"You plan."}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runPersonaValidatePath(&buf, path); err != nil {
		t.Fatalf("valid persona rejected: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "foundry_planner") {
		t.Errorf("output does not name the persona: %q", buf.String())
	}
}

// TestPersonaValidate_RejectsCoreCollision pins decision 1 for the CLI: a file
// that reuses a core persona's id fails validation and the failure is named.
func TestPersonaValidate_RejectsCoreCollision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shadow.json")
	body := `{"personas":[{"id":"designer","name":"Fake","description":"x","system_prompt_text":"mine"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := runPersonaValidatePath(&buf, path)
	if err == nil {
		t.Fatalf("core-id collision validated; output: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "designer") || !strings.Contains(buf.String(), "immutable") {
		t.Errorf("output does not explain the collision: %q", buf.String())
	}
}

// TestPersonaValidate_ReportsMalformedAndKeepsTheRest pins the resilience rule
// at the CLI: one bad file is named, the good one still validates.
func TestPersonaValidate_ReportsMalformedAndKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{"personas":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	good := `{"personas":[{"id":"fine_one","name":"Fine","description":"ok","system_prompt_text":"hi"}]}`
	if err := os.WriteFile(filepath.Join(dir, "good.json"), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := runPersonaValidatePath(&buf, dir)
	if err == nil {
		t.Fatal("a malformed file did not fail validation")
	}
	out := buf.String()
	if !strings.Contains(out, "broken.json") {
		t.Errorf("malformed file not named: %q", out)
	}
	if !strings.Contains(out, "fine_one") {
		t.Errorf("the valid persona was not reported: %q", out)
	}
}

// TestPersonaDirs_NamesBothScopes pins the directory report.
func TestPersonaDirs_NamesBothScopes(t *testing.T) {
	var buf bytes.Buffer
	if err := runPersonaDirs(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "User scope:") {
		t.Errorf("output = %q", buf.String())
	}
}
