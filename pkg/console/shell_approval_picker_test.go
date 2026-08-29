package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func makeTestParts(count int) []ShellPartInfo {
	parts := make([]ShellPartInfo, count)
	for i := range parts {
		parts[i] = ShellPartInfo{
			ID:        "part-" + fmt.Sprintf("%d", i),
			Text:      "echo hello" + fmt.Sprintf("%d", i),
			Kind:      "unknown",
			Semantic:  "Print hello" + fmt.Sprintf("%d", i),
			RiskLabel: "LOW",
		}
	}
	return parts
}

func TestPromptShellApprovalPartsIO_Empty(t *testing.T) {
	decisions, err := promptShellApprovalPartsIO(context.Background(), nil, strings.NewReader(""), &discardingWriter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decisions) != 0 {
		t.Errorf("expected empty map, got %d entries", len(decisions))
	}
}

func TestPromptShellApprovalPartsIO_SingleApprove(t *testing.T) {
	parts := makeTestParts(1)
	in := strings.NewReader("y\n")
	var out strings.Builder
	decisions, err := promptShellApprovalPartsIO(context.Background(), parts, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	if !decisions["part-0"] {
		t.Error("expected part-0 to be approved")
	}
}

func TestPromptShellApprovalPartsIO_MultiPartMixed(t *testing.T) {
	parts := makeTestParts(2)
	in := strings.NewReader("y\nn\n")
	var out strings.Builder
	decisions, err := promptShellApprovalPartsIO(context.Background(), parts, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(decisions))
	}
	if !decisions["part-0"] {
		t.Error("expected part-0 to be approved")
	}
	if decisions["part-1"] {
		t.Error("expected part-1 to be rejected")
	}
}

func TestPromptShellApprovalPartsIO_BulkAccept(t *testing.T) {
	parts := makeTestParts(3)
	in := strings.NewReader("a\n")
	var out strings.Builder
	decisions, err := promptShellApprovalPartsIO(context.Background(), parts, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 3; i++ {
		id := "part-" + fmt.Sprintf("%d", i)
		if !decisions[id] {
			t.Errorf("expected %s to be approved (bulk accept)", id)
		}
	}
}

func TestPromptShellApprovalPartsIO_BulkReject(t *testing.T) {
	parts := makeTestParts(3)
	in := strings.NewReader("r\n")
	var out strings.Builder
	decisions, err := promptShellApprovalPartsIO(context.Background(), parts, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 3; i++ {
		id := "part-" + fmt.Sprintf("%d", i)
		if decisions[id] {
			t.Errorf("expected %s to be rejected (bulk reject)", id)
		}
	}
}

func TestPromptShellApprovalPartsIO_EOF(t *testing.T) {
	parts := makeTestParts(3)
	in := strings.NewReader("") // EOF immediately
	var out strings.Builder
	decisions, err := promptShellApprovalPartsIO(context.Background(), parts, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 3; i++ {
		id := "part-" + fmt.Sprintf("%d", i)
		if decisions[id] {
			t.Errorf("expected %s to be denied on EOF (safe default)", id)
		}
	}
}

func TestPromptShellApprovalPartsIO_Quit(t *testing.T) {
	parts := makeTestParts(3)
	in := strings.NewReader("y\nq\n")
	var out strings.Builder
	decisions, err := promptShellApprovalPartsIO(context.Background(), parts, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decisions) != 3 {
		t.Fatalf("expected 3 decisions, got %d", len(decisions))
	}
	if !decisions["part-0"] {
		t.Error("expected part-0 to be approved before quit")
	}
	if decisions["part-1"] {
		t.Error("expected part-1 to be denied after quit")
	}
	if decisions["part-2"] {
		t.Error("expected part-2 to be denied after quit")
	}
}

func TestPromptShellApprovalPartsIO_InvalidInputThenApprove(t *testing.T) {
	parts := makeTestParts(1)
	in := strings.NewReader("xyz\ny\n")
	var out strings.Builder
	decisions, err := promptShellApprovalPartsIO(context.Background(), parts, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decisions["part-0"] {
		t.Error("expected part-0 to be approved after invalid input")
	}
	// Verify invalid input message appeared in output.
	if !strings.Contains(out.String(), "invalid input") {
		t.Error("expected 'invalid input' message in output")
	}
}

// discardingWriter is an io.Writer that discards everything.
type discardingWriter struct{}

func (d *discardingWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestPromptShellApprovalPartsIO_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	parts := []ShellPartInfo{
		{ID: "part-0", Text: "echo hi", Kind: "unknown", RiskLabel: "LOW", Semantic: "x"},
	}
	_, err := promptShellApprovalPartsIO(ctx, parts, strings.NewReader("y\ny\n"), io.Discard)
	if err == nil {
		t.Fatal("expected non-nil error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// A pipe that never delivers a line blocks the picker mid-prompt;
// cancelling the ctx must unblock it with an error and deny the rest.
func TestPromptShellApprovalPartsIO_ContextCancelledMidPrompt(t *testing.T) {
	parts := makeTestParts(3)
	pr, pw := io.Pipe()
	defer pw.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out strings.Builder
	var decisions map[string]bool
	var err error
	done := make(chan struct{})
	go func() {
		decisions, err = promptShellApprovalPartsIO(ctx, parts, pr, &out)
		close(done)
	}()

	// Let the first part's prompt render and block on its read, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("picker did not return after context cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	for i := 0; i < 3; i++ {
		id := "part-" + fmt.Sprintf("%d", i)
		if decisions[id] {
			t.Errorf("expected %s to be denied after context cancellation", id)
		}
	}
	if !strings.Contains(out.String(), "interrupted") {
		t.Error("expected interruption notice in output")
	}
}

func TestPromptShellApprovalParts_SuspendResume(t *testing.T) {
	var calls []string
	prevSuspend := shellPickerSuspend
	prevResume := shellPickerResume
	shellPickerSuspend = func() { calls = append(calls, "suspend") }
	shellPickerResume = func() { calls = append(calls, "resume") }
	defer func() {
		shellPickerSuspend = prevSuspend
		shellPickerResume = prevResume
	}()

	// Empty parts returns before any stdin read, so os.Stdin is untouched.
	_, err := PromptShellApprovalParts(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"suspend", "resume"}) {
		t.Errorf("expected [suspend resume] in order, got %v", calls)
	}
}
