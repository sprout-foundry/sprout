package configuration

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// captureStdout captures output written to stdout during function execution.
//
// Uses a goroutine to drain the read end of the pipe concurrently with
// writing, so callers can emit arbitrarily large output without deadlocking
// the writer when the OS pipe buffer (~64 KiB on Linux) fills.
func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(fmt.Sprintf("captureStdout: pipe: %v", err))
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf strings.Builder
		tmp := make([]byte, 4096)
		for {
			n, readErr := r.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}
			if readErr != nil {
				break
			}
		}
		done <- buf.String()
	}()

	defer func() {
		_ = w.Close()
		os.Stdout = old
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

func TestShowWelcomeMessage(t *testing.T) {
	output := captureStdout(ShowWelcomeMessage)

	// Verify daemon command is mentioned
	if !strings.Contains(output, "sprout agent -d") {
		t.Errorf("ShowWelcomeMessage() should contain 'sprout agent -d', got:\n%s", output)
	}

	// Verify web is mentioned (case insensitive)
	lowerOutput := strings.ToLower(output)
	if !strings.Contains(lowerOutput, "web") {
		t.Errorf("ShowWelcomeMessage() should contain 'web' (case insensitive), got:\n%s", output)
	}

	// Verify port 56000 is mentioned
	if !strings.Contains(output, "56000") {
		t.Errorf("ShowWelcomeMessage() should contain port '56000', got:\n%s", output)
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
	if !strings.Contains(output, "sprout agent -d") {
		t.Errorf("ShowNextSteps(%q) should contain 'sprout agent -d', got:\n%s", provider, output)
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
	if !strings.Contains(output, "sprout agent -d") {
		t.Errorf("ShowNextSteps(%q) should mention 'sprout agent -d' to configure providers, got:\n%s", provider, output)
	}

	// Should mention that AI features are not available
	if !strings.Contains(output, "not available") {
		t.Errorf("ShowNextSteps(%q) should indicate AI features are not available, got:\n%s", provider, output)
	}
}

// TestCaptureStdout_LargeOutput exercises captureStdout with output that
// exceeds the OS pipe buffer (64 KiB on Linux). Without a concurrent
// reader, the writer inside fn() would block once the buffer fills.
func TestCaptureStdout_LargeOutput(t *testing.T) {
	const size = 256 * 1024 // 4x the typical pipe buffer
	want := strings.Repeat("x", size)

	out := captureStdout(func() {
		fmt.Print(want)
	})

	if len(out) != size {
		t.Fatalf("captured %d bytes, want %d", len(out), size)
	}
	if out != want {
		t.Fatal("output content mismatch")
	}
}
