package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// --- ExecuteShellCommandBackground tests ---

func TestExecuteShellCommandBackground_Success(t *testing.T) {
	tm := &mockTerminalManager{
		executeBackgroundFunc: func(ctx context.Context, chatID, command string) (string, error) {
			if chatID != "test-session-123" {
				t.Errorf("expected chatID 'test-session-123', got %q", chatID)
			}
			if command != "npm run dev" {
				t.Errorf("expected command 'npm run dev', got %q", command)
			}
			return "bg-npm-dev-aabbccdd", nil
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	result, err := ExecuteShellCommandBackground(ctx, "npm run dev", "test-session-123")
	if err != nil {
		t.Fatalf("ExecuteShellCommandBackground failed: %v", err)
	}

	// Parse the JSON result.
	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v (got: %q)", err, result)
	}

	if parsed["session_id"] != "bg-npm-dev-aabbccdd" {
		t.Errorf("expected session_id 'bg-npm-dev-aabbccdd', got %q", parsed["session_id"])
	}
	if parsed["status"] != "running" {
		t.Errorf("expected status 'running', got %q", parsed["status"])
	}
}

func TestExecuteShellCommandBackground_Success_DefaultChatID(t *testing.T) {
	tm := &mockTerminalManager{
		executeBackgroundFunc: func(ctx context.Context, chatID, command string) (string, error) {
			// When sessionID is empty, chatID should default to "default".
			if chatID != "default" {
				t.Errorf("expected chatID 'default' when sessionID is empty, got %q", chatID)
			}
			return "bg-echo-aabbccdd", nil
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	result, err := ExecuteShellCommandBackground(ctx, "echo hello", "")
	if err != nil {
		t.Fatalf("ExecuteShellCommandBackground failed: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if parsed["status"] != "running" {
		t.Errorf("expected status 'running', got %q", parsed["status"])
	}
}

func TestExecuteShellCommandBackground_EmptyCommand(t *testing.T) {
	tm := &mockTerminalManager{}
	ctx := WithTerminalManager(context.Background(), tm)

	_, err := ExecuteShellCommandBackground(ctx, "", "session-1")
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error message to mention 'empty', got %q", err.Error())
	}
}

func TestExecuteShellCommandBackground_WhitespaceOnlyCommand(t *testing.T) {
	tm := &mockTerminalManager{}
	ctx := WithTerminalManager(context.Background(), tm)

	_, err := ExecuteShellCommandBackground(ctx, "   \n\t  ", "session-1")
	if err == nil {
		t.Fatal("expected error for whitespace-only command")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error message to mention 'empty', got %q", err.Error())
	}
}

func TestExecuteShellCommandBackground_NoTerminalManager(t *testing.T) {
	ctx := context.Background() // No terminal manager in context

	_, err := ExecuteShellCommandBackground(ctx, "echo hello", "session-1")
	if err == nil {
		t.Fatal("expected error when no terminal manager is available")
	}
	if !strings.Contains(err.Error(), "WebUI") {
		t.Errorf("expected error message to mention 'WebUI', got %q", err.Error())
	}
}

func TestExecuteShellCommandBackground_SessionCreationFails(t *testing.T) {
	tm := &mockTerminalManager{
		executeBackgroundFunc: func(ctx context.Context, chatID, command string) (string, error) {
			return "", fmt.Errorf("failed to create PTY session")
		},
	}
	ctx := WithTerminalManager(context.Background(), tm)

	_, err := ExecuteShellCommandBackground(ctx, "echo hello", "session-1")
	if err == nil {
		t.Fatal("expected error when background session creation fails")
	}
	if !strings.Contains(err.Error(), "execute background command") {
		t.Errorf("expected error message to contain 'execute background command', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "failed to create PTY session") {
		t.Errorf("expected error message to contain underlying cause 'failed to create PTY session', got %q", err.Error())
	}
}
