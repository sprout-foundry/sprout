package cmd

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAgentInteractiveModeExitHandling(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "ledit-agent-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to temp dir
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Create a minimal main.go to make the binary buildable (mock)
	mainContent := `package main

import "github.com/alantheprice/ledit/cmd"

func main() {
	cmd.Execute()
}`
	if err := os.WriteFile("main.go", []byte(mainContent), 0644); err != nil {
		t.Fatalf("Failed to write main.go: %v", err)
	}

	// Build the binary
	buildCmd := exec.Command("go", "build", "-o", "ledit")
	buildCmd.Dir = tmpDir
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build: %v\nOutput: %s", err, output)
	}

	// Test cases: inputs that should trigger exit
	testCases := []struct {
		name       string
		input      string
		expected   string // Expected output indicating exit
		shouldExit bool
	}{
		{
			name:       "plain exit",
			input:      "exit\n",
			expected:   "👋 Exiting interactive mode",
			shouldExit: true,
		},
		{
			name:       "plain quit",
			input:      "quit\n",
			expected:   "👋 Exiting interactive mode",
			shouldExit: true,
		},
		{
			name:       "slash exit",
			input:      "/exit\n",
			expected:   "👋 Exiting interactive mode",
			shouldExit: true,
		},
		{
			name:       "slash quit",
			input:      "/quit\n",
			expected:   "👋 Exiting interactive mode",
			shouldExit: true,
		},
		{
			name:       "q shortcut",
			input:      "/q\n",
			expected:   "👋 Exiting interactive mode",
			shouldExit: true,
		},
		{
			name:       "plain q",
			input:      "q\n", // Should not exit, as it's not handled as shortcut without slash
			expected:   "",
			shouldExit: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Run the agent command in interactive mode
			cmd := exec.Command("./ledit", "agent")
			cmd.Dir = tmpDir

			// Set up pipes
			stdin, _ := cmd.StdinPipe()
			stdout, _ := cmd.StdoutPipe()
			_, _ = cmd.StderrPipe() // Ignore stderr to avoid unused var

			if err := cmd.Start(); err != nil {
				t.Fatalf("Failed to start command: %v", err)
			}

			// Wait a bit for the prompt to appear
			time.Sleep(200 * time.Millisecond)

			// Write input in goroutine to simulate user input
			go func() {
				time.Sleep(100 * time.Millisecond)
				_, _ = stdin.Write([]byte(tc.input))
				stdin.Close()
			}()

			// Read output in goroutine
			var outputBuf bytes.Buffer
			outputDone := make(chan bool)
			go func() {
				defer close(outputDone)
				reader := bufio.NewReader(stdout)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						if err != io.EOF {
							t.Logf("Read error: %v", err)
						}
						break
					}
					outputBuf.WriteString(line)
					if tc.shouldExit && strings.Contains(line, tc.expected) {
						break
					}
				}
			}()

			// Wait for process or timeout
			select {
			case err := <-func() chan error {
				ch := make(chan error, 1)
				go func() {
					ch <- cmd.Wait()
				}()
				return ch
			}():
				if tc.shouldExit {
					if err != nil {
						t.Errorf("Expected successful exit for %s, got: %v", tc.name, err)
					}
					gotOutput := outputBuf.String()
					if !strings.Contains(gotOutput, tc.expected) {
						t.Errorf("Expected output to contain '%s' for %s, got: %s", tc.expected, tc.name, gotOutput)
					}
				} else {
					// For non-exit, expect it didn't exit immediately, but this is approximate
					if err == nil {
						t.Logf("Process exited unexpectedly for non-exit case: %s", tc.name)
					}
				}
			case <-time.After(3 * time.Second):
				if tc.shouldExit {
					t.Errorf("Test %s timed out waiting for exit", tc.name)
				} else {
					t.Logf("Non-exit test %s completed (process still running or timed out)", tc.name)
				}
				cmd.Process.Kill()
			}

			// Wait for output goroutine and close pipes
			<-outputDone
			stdout.Close()
		})
	}
}

func TestAgentSlashCommandRouting(t *testing.T) {
	// Simple smoke test - integration covered above
	testCases := []struct {
		name    string
		input   string
		handled bool
	}{
		{"plain exit", "exit", true},
		{"plain quit", "quit", true},
		{"slash exit", "/exit", true},
		{"slash quit", "/quit", true},
		{"q shortcut", "/q", true},
		{"non-exit", "hello", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This is a placeholder - actual verification via integration test above
			if tc.handled {
				t.Logf("Input '%s' is expected to be handled as exit command", tc.input)
			} else {
				t.Logf("Input '%s' is not expected to trigger exit", tc.input)
			}
		})
	}
}