package configuration

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout captures output written to stdout during function execution
func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestShowWelcomeMessage(t *testing.T) {
	output := captureStdout(ShowWelcomeMessage)

	// Verify daemon command is mentioned
	if !strings.Contains(output, "ledit agent -d") {
		t.Errorf("ShowWelcomeMessage() should contain 'ledit agent -d', got:\n%s", output)
	}

	// Verify web is mentioned (case insensitive)
	lowerOutput := strings.ToLower(output)
	if !strings.Contains(lowerOutput, "web") {
		t.Errorf("ShowWelcomeMessage() should contain 'web' (case insensitive), got:\n%s", output)
	}

	// Verify port 54000 is mentioned
	if !strings.Contains(output, "54000") {
		t.Errorf("ShowWelcomeMessage() should contain port '54000', got:\n%s", output)
	}

	// Verify the webui section is prominently placed early in the output
	lines := strings.Split(output, "\n")
	// The webui section with [i] Get started with the web-based code editor should appear
	// before the "Recommended for beginners" section
	var webuiSectionIndex, beginnersIndex int
	webuiSectionIndex = -1
	beginnersIndex = -1

	for i, line := range lines {
		if strings.Contains(line, "Get started with the web-based code editor") {
			webuiSectionIndex = i
		}
		if strings.Contains(line, "Recommended for beginners") {
			beginnersIndex = i
		}
	}

	if webuiSectionIndex == -1 {
		t.Errorf("ShowWelcomeMessage() should contain 'Get started with the web-based code editor', got:\n%s", output)
	}

	if webuiSectionIndex != -1 && beginnersIndex != -1 && webuiSectionIndex > beginnersIndex {
		t.Errorf("ShowWelcomeMessage() should show webui section before 'Recommended for beginners' section, got:\n%s", output)
	}
}

func TestShowNextSteps_NormalProvider(t *testing.T) {
	provider := "openrouter"
	configDir := "/tmp/ledit-config"

	output := captureStdout(func() {
		ShowNextSteps(provider, configDir)
	})

	// Verify daemon command is the first recommended step
	if !strings.Contains(output, "ledit agent -d") {
		t.Errorf("ShowNextSteps(%q) should contain 'ledit agent -d', got:\n%s", provider, output)
	}

	// Verify it's marked as recommended
	if !strings.Contains(output, "(recommended)") {
		t.Errorf("ShowNextSteps(%q) should mark webui as recommended, got:\n%s", provider, output)
	}

	// Verify web editor is mentioned
	lowerOutput := strings.ToLower(output)
	if !strings.Contains(lowerOutput, "web-based code editor") {
		t.Errorf("ShowNextSteps(%q) should mention web-based code editor, got:\n%s", provider, output)
	}
}

func TestShowNextSteps_EditorOnly(t *testing.T) {
	provider := "editor"
	configDir := "/tmp/ledit-config"

	output := captureStdout(func() {
		ShowNextSteps(provider, configDir)
	})

	// Should mention editor-only mode
	if !strings.Contains(output, "editor-only mode") {
		t.Errorf("ShowNextSteps(%q) should mention editor-only mode, got:\n%s", provider, output)
	}

	// Should provide guidance to enable AI features via webui
	if !strings.Contains(output, "ledit agent -d") {
		t.Errorf("ShowNextSteps(%q) should mention 'ledit agent -d' to configure providers, got:\n%s", provider, output)
	}

	// Should mention that AI features are not available
	if !strings.Contains(output, "not available") {
		t.Errorf("ShowNextSteps(%q) should indicate AI features are not available, got:\n%s", provider, output)
	}
}
