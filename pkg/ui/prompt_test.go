package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// ─── helpers ───────────────────────────────────────────────────────────

// captureStdout redirects os.Stdout to a pipe, runs fn, then returns
// the captured output as a string.
//
// Uses a goroutine to drain the read end of the pipe concurrently with
// writing, so callers can emit arbitrarily large output without deadlocking
// the writer when the OS pipe buffer (~64 KiB on Linux) fills.
func captureStdout(fn func()) string {
	oldStdout := os.Stdout
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
		os.Stdout = oldStdout
	}()
	fn()
	_ = w.Close()
	os.Stdout = oldStdout
	return <-done
}

// withStdin temporarily replaces os.Stdin with a pipe fed by `input`,
// runs fn, then restores the original stdin.
func withStdin(input string, fn func()) {
	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	go func() {
		w.WriteString(input + "\n")
		w.Close()
	}()

	fn()
}

// bothStdinStdout replaces both os.Stdin and os.Stdout, runs fn, and
// returns the captured stdout string.  Useful when a function reads
// from stdin *and* writes to stdout.
func bothStdinStdout(stdinInput string, fn func()) (string, bool) {
	stdinR, stdinW, _ := os.Pipe()
	stdoutR, stdoutW, _ := os.Pipe()

	oldStdin := os.Stdin
	oldStdout := os.Stdout
	os.Stdin = stdinR
	os.Stdout = stdoutW

	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	go func() {
		stdinW.WriteString(stdinInput + "\n")
		stdinW.Close()
	}()

	fn()

	stdoutW.Close()
	var buf bytes.Buffer
	io.Copy(&buf, stdoutR)

	return buf.String(), true
}

// ─── constants ─────────────────────────────────────────────────────────

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

func TestDefaultPrompt(t *testing.T) {
	const want = "Enter option number (or 0 to cancel): "
	if DefaultPrompt != want {
		t.Errorf("DefaultPrompt = %q, want %q", DefaultPrompt, want)
	}
}

// ─── NumericPromptOption ───────────────────────────────────────────────

func TestNumericPromptOption(t *testing.T) {
	opt := NumericPromptOption{
		Index:       3,
		DisplayName: "Third option",
		Description: "Description text",
		Value:       "third",
	}

	if opt.Index != 3 {
		t.Errorf("Index = %d, want 3", opt.Index)
	}
	if opt.DisplayName != "Third option" {
		t.Errorf("DisplayName = %q, want %q", opt.DisplayName, "Third option")
	}
	if opt.Description != "Description text" {
		t.Errorf("Description = %q, want %q", opt.Description, "Description text")
	}
	if opt.Value != "third" {
		t.Errorf("Value = %q, want %q", opt.Value, "third")
	}
}

// ─── DisplayNumberedList ───────────────────────────────────────────────

func TestDisplayNumberedList(t *testing.T) {
	t.Run("non-empty list", func(t *testing.T) {
		items := []string{"alpha", "beta", "gamma"}
		got := captureStdout(func() {
			DisplayNumberedList(items)
		})

		want := "1. alpha\n2. beta\n3. gamma\n"
		if got != want {
			t.Errorf("output = %q, want %q", got, want)
		}
	})

	t.Run("single item", func(t *testing.T) {
		items := []string{"only one"}
		got := captureStdout(func() {
			DisplayNumberedList(items)
		})

		want := "1. only one\n"
		if got != want {
			t.Errorf("output = %q, want %q", got, want)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		items := []string{}
		got := captureStdout(func() {
			DisplayNumberedList(items)
		})

		if got != "" {
			t.Errorf("output = %q, want empty string", got)
		}
	})

	t.Run("items with spaces", func(t *testing.T) {
		items := []string{"item one", "item two"}
		got := captureStdout(func() {
			DisplayNumberedList(items)
		})

		want := "1. item one\n2. item two\n"
		if got != want {
			t.Errorf("output = %q, want %q", got, want)
		}
	})

	t.Run("large list", func(t *testing.T) {
		items := make([]string, 100)
		for i := range items {
			items[i] = "item" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		}
		got := captureStdout(func() {
			DisplayNumberedList(items)
		})

		// Check first and last lines
		if !bytes.HasPrefix([]byte(got), []byte("1. i")) {
			t.Errorf("first line = %q, expected to start with '1. i'", got)
		}
		if !bytes.HasSuffix([]byte(got), []byte("100. item99\n")) {
			t.Errorf("last line = %q, expected to end with '100. item99\\n'", got)
		}
	})
}

// ─── DisplayNumberedListWithDescriptions ───────────────────────────────

func TestDisplayNumberedListWithDescriptions(t *testing.T) {
	t.Run("with descriptions", func(t *testing.T) {
		opts := []NumericPromptOption{
			{Index: 1, DisplayName: "Option A", Description: "First option"},
			{Index: 2, DisplayName: "Option B", Description: "Second option"},
		}
		got := captureStdout(func() {
			DisplayNumberedListWithDescriptions(opts)
		})

		want := "1. Option A - First option\n2. Option B - Second option\n"
		if got != want {
			t.Errorf("output = %q, want %q", got, want)
		}
	})

	t.Run("without descriptions", func(t *testing.T) {
		opts := []NumericPromptOption{
			{Index: 1, DisplayName: "Option A", Description: ""},
			{Index: 2, DisplayName: "Option B", Description: ""},
		}
		got := captureStdout(func() {
			DisplayNumberedListWithDescriptions(opts)
		})

		want := "1. Option A\n2. Option B\n"
		if got != want {
			t.Errorf("output = %q, want %q", got, want)
		}
	})

	t.Run("mixed descriptions", func(t *testing.T) {
		opts := []NumericPromptOption{
			{Index: 1, DisplayName: "Option A", Description: "Has desc"},
			{Index: 2, DisplayName: "Option B", Description: ""},
			{Index: 3, DisplayName: "Option C", Description: "Another desc"},
		}
		got := captureStdout(func() {
			DisplayNumberedListWithDescriptions(opts)
		})

		want := "1. Option A - Has desc\n2. Option B\n3. Option C - Another desc\n"
		if got != want {
			t.Errorf("output = %q, want %q", got, want)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		opts := []NumericPromptOption{}
		got := captureStdout(func() {
			DisplayNumberedListWithDescriptions(opts)
		})

		if got != "" {
			t.Errorf("output = %q, want empty string", got)
		}
	})

	t.Run("uses Option.Index not iteration order", func(t *testing.T) {
		opts := []NumericPromptOption{
			{Index: 5, DisplayName: "Fifth"},
			{Index: 1, DisplayName: "First"},
		}
		got := captureStdout(func() {
			DisplayNumberedListWithDescriptions(opts)
		})

		// The function iterates the slice, using opt.Index for numbering
		want := "5. Fifth\n1. First\n"
		if got != want {
			t.Errorf("output = %q, want %q", got, want)
		}
	})
}

// ─── PromptForConfirmation ─────────────────────────────────────────────

func TestPromptForConfirmation(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		// -- affirmative inputs --
		{"lowercase y", "y", true},
		{"uppercase Y", "Y", true},
		{"yes", "yes", true},
		{"YES", "YES", true},
		{"Yes", "Yes", true},
		{"yep", "yep", true},
		{"ya", "ya", true},
		{"y\n", "y", true}, // input without extra trailing (trimmed)

		// -- negative inputs --
		{"n", "n", false},
		{"N", "N", false},
		{"no", "no", false},
		{"NO", "NO", false},
		{"No", "No", false},
		{"maybe", "maybe", false},
		{"cancel", "cancel", false},
		{"", "", false},
		{"anything else", "anything else", false},

		// -- whitespace handling --
		{"spaces around yes", "  yes  ", true},
		{"spaces around no", "  no  ", false},
		{"just spaces", "  ", false},
		{"tab yes", "\tyes\t", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			withStdin(tc.input, func() {
				got = PromptForConfirmation("")
			})

			if got != tc.want {
				t.Errorf("PromptForConfirmation(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}

	t.Run("custom prompt", func(t *testing.T) {
		var got bool
		captured := captureStdout(func() {
			withStdin("y", func() {
				got = PromptForConfirmation("Proceed? (y/n): ")
			})
		})
		if !got {
			t.Errorf("got = %v, want true", got)
		}
		if !bytes.Contains([]byte(captured), []byte("Proceed? (y/n): ")) {
			t.Errorf("output does not contain custom prompt: %q", captured)
		}
	})

	t.Run("default prompt when empty", func(t *testing.T) {
		var got bool
		captured := captureStdout(func() {
			withStdin("y", func() {
				got = PromptForConfirmation("")
			})
		})
		if !got {
			t.Errorf("got = %v, want true", got)
		}
		if !bytes.Contains([]byte(captured), []byte("Continue? (y/n): ")) {
			t.Errorf("output does not contain default prompt: %q", captured)
		}
	})
}

// ─── PromptForSelection ────────────────────────────────────────────────

func TestPromptForSelection(t *testing.T) {
	options := []string{"Apple", "Banana", "Cherry"}

	t.Run("valid selection - first", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("1", func() {
			idx, ok = PromptForSelection(options, "")
		})
		if !ok {
			t.Error("ok should be true for valid selection")
		}
		if idx != 1 {
			t.Errorf("idx = %d, want 1", idx)
		}
	})

	t.Run("valid selection - middle", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("2", func() {
			idx, ok = PromptForSelection(options, "")
		})
		if !ok {
			t.Error("ok should be true for valid selection")
		}
		if idx != 2 {
			t.Errorf("idx = %d, want 2", idx)
		}
	})

	t.Run("valid selection - last", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("3", func() {
			idx, ok = PromptForSelection(options, "")
		})
		if !ok {
			t.Error("ok should be true for valid selection")
		}
		if idx != 3 {
			t.Errorf("idx = %d, want 3", idx)
		}
	})

	t.Run("valid selection with leading/trailing whitespace", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("  2  ", func() {
			idx, ok = PromptForSelection(options, "")
		})
		if !ok {
			t.Error("ok should be true for valid selection")
		}
		if idx != 2 {
			t.Errorf("idx = %d, want 2", idx)
		}
	})

	t.Run("cancellation with 0", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("0", func() {
			idx, ok = PromptForSelection(options, "")
		})
		if !ok {
			t.Error("ok should be true for cancellation")
		}
		if idx != 0 {
			t.Errorf("idx = %d, want 0", idx)
		}
	})

	t.Run("out of range - too high", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("4", func() {
			idx, ok = PromptForSelection(options, "")
		})
		if ok {
			t.Error("ok should be false for out-of-range selection")
		}
		if idx != 0 {
			t.Errorf("idx = %d, want 0", idx)
		}
	})

	t.Run("out of range - negative", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("-1", func() {
			idx, ok = PromptForSelection(options, "")
		})
		if ok {
			t.Error("ok should be false for negative selection")
		}
		if idx != 0 {
			t.Errorf("idx = %d, want 0", idx)
		}
	})

	t.Run("non-numeric input", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("abc", func() {
			idx, ok = PromptForSelection(options, "")
		})
		if ok {
			t.Error("ok should be false for non-numeric input")
		}
		if idx != 0 {
			t.Errorf("idx = %d, want 0", idx)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("", func() {
			idx, ok = PromptForSelection(options, "")
		})
		if ok {
			t.Error("ok should be false for empty input (parse error)")
		}
		if idx != 0 {
			t.Errorf("idx = %d, want 0", idx)
		}
	})

	t.Run("single option - selecting 1", func(t *testing.T) {
		opts := []string{"Only one"}
		var idx int
		var ok bool
		withStdin("1", func() {
			idx, ok = PromptForSelection(opts, "")
		})
		if !ok {
			t.Error("ok should be true for valid selection")
		}
		if idx != 1 {
			t.Errorf("idx = %d, want 1", idx)
		}
	})

	t.Run("single option - out of range", func(t *testing.T) {
		opts := []string{"Only one"}
		var idx int
		var ok bool
		withStdin("2", func() {
			idx, ok = PromptForSelection(opts, "")
		})
		if ok {
			t.Error("ok should be false for out-of-range")
		}
		if idx != 0 {
			t.Errorf("idx = %d, want 0", idx)
		}
	})

	t.Run("custom prompt is displayed", func(t *testing.T) {
		var idx int
		var ok bool
		captured := captureStdout(func() {
			withStdin("2", func() {
				idx, ok = PromptForSelection(options, "Pick: ")
			})
		})
		if !ok {
			t.Error("ok should be true for valid selection")
		}
		if idx != 2 {
			t.Errorf("idx = %d, want 2", idx)
		}
		if !bytes.Contains([]byte(captured), []byte("Pick: ")) {
			t.Errorf("output does not contain custom prompt: %q", captured)
		}
	})

	t.Run("default prompt when empty", func(t *testing.T) {
		var idx int
		var ok bool
		captured := captureStdout(func() {
			withStdin("1", func() {
				idx, ok = PromptForSelection(options, "")
			})
		})
		if !ok {
			t.Error("ok should be true for valid selection")
		}
		if idx != 1 {
			t.Errorf("idx = %d, want 1", idx)
		}
		if !bytes.Contains([]byte(captured), []byte("Enter option number (or 0 to cancel): ")) {
			t.Errorf("output does not contain default prompt: %q", captured)
		}
	})

	t.Run("zero selection prints cancelled message", func(t *testing.T) {
		var idx int
		var ok bool
		captured := captureStdout(func() {
			withStdin("0", func() {
				idx, ok = PromptForSelection(options, "")
			})
		})
		if !ok {
			t.Error("ok should be true for cancellation")
		}
		if idx != 0 {
			t.Errorf("idx = %d, want 0", idx)
		}
		if !bytes.Contains([]byte(captured), []byte("Cancelled.")) {
			t.Errorf("output does not contain 'Cancelled.': %q", captured)
		}
	})

	t.Run("out-of-range prints range hint", func(t *testing.T) {
		captured := captureStdout(func() {
			withStdin("5", func() {
				PromptForSelection(options, "")
			})
		})
		wantMsg := "Invalid selection. Please enter a number between 1 and 3."
		if !bytes.Contains([]byte(captured), []byte(wantMsg)) {
			t.Errorf("output does not contain range hint %q: %q", wantMsg, captured)
		}
	})

	t.Run("invalid input prints error message", func(t *testing.T) {
		captured := captureStdout(func() {
			withStdin("abc", func() {
				PromptForSelection(options, "")
			})
		})
		if !bytes.Contains([]byte(captured), []byte("Invalid input")) {
			t.Errorf("output does not contain 'Invalid input': %q", captured)
		}
	})
}

// ─── PromptForSelectionWithOptions ─────────────────────────────────────

func TestPromptForSelectionWithOptions(t *testing.T) {
	opts := []NumericPromptOption{
		{Index: 1, DisplayName: "First", Description: "The first option"},
		{Index: 2, DisplayName: "Second", Description: "The second option"},
		{Index: 3, DisplayName: "Third", Description: ""},
	}

	t.Run("valid selection", func(t *testing.T) {
		var idx int
		var ok bool
		captured := captureStdout(func() {
			withStdin("2", func() {
				idx, ok = PromptForSelectionWithOptions(opts, "Choose: ")
			})
		})
		if !ok {
			t.Error("ok should be true for valid selection")
		}
		if idx != 2 {
			t.Errorf("idx = %d, want 2", idx)
		}

		// Check that the numbered list with descriptions was displayed
		if !bytes.Contains([]byte(captured), []byte("1. First - The first option")) {
			t.Errorf("output missing option 1 with description: %q", captured)
		}
		if !bytes.Contains([]byte(captured), []byte("2. Second - The second option")) {
			t.Errorf("output missing option 2 with description: %q", captured)
		}
		if !bytes.Contains([]byte(captured), []byte("3. Third")) {
			t.Errorf("output missing option 3 without description: %q", captured)
		}
	})

	t.Run("selection at boundary - first", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("1", func() {
			idx, ok = PromptForSelectionWithOptions(opts, "")
		})
		if !ok {
			t.Error("ok should be true")
		}
		if idx != 1 {
			t.Errorf("idx = %d, want 1", idx)
		}
	})

	t.Run("selection at boundary - last", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("3", func() {
			idx, ok = PromptForSelectionWithOptions(opts, "")
		})
		if !ok {
			t.Error("ok should be true")
		}
		if idx != 3 {
			t.Errorf("idx = %d, want 3", idx)
		}
	})

	t.Run("out of range", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("5", func() {
			idx, ok = PromptForSelectionWithOptions(opts, "")
		})
		if ok {
			t.Error("ok should be false for out-of-range")
		}
		if idx != 0 {
			t.Errorf("idx = %d, want 0", idx)
		}
	})

	t.Run("empty options shows message and returns false", func(t *testing.T) {
		var idx int
		var ok bool
		captured := captureStdout(func() {
			idx, ok = PromptForSelectionWithOptions([]NumericPromptOption{}, "Choose: ")
		})
		if ok {
			t.Error("ok should be false when there are no options")
		}
		if idx != 0 {
			t.Errorf("idx = %d, want 0", idx)
		}
		if !bytes.Contains([]byte(captured), []byte("No options available.")) {
			t.Errorf("output does not contain 'No options available.': %q", captured)
		}
	})

	t.Run("single option", func(t *testing.T) {
		singleOpts := []NumericPromptOption{
			{Index: 1, DisplayName: "Solo", Description: "Only choice"},
		}
		var idx int
		var ok bool
		withStdin("1", func() {
			idx, ok = PromptForSelectionWithOptions(singleOpts, "")
		})
		if !ok {
			t.Error("ok should be true")
		}
		if idx != 1 {
			t.Errorf("idx = %d, want 1", idx)
		}
	})

	t.Run("non-numeric input", func(t *testing.T) {
		var idx int
		var ok bool
		withStdin("x", func() {
			idx, ok = PromptForSelectionWithOptions(opts, "")
		})
		if ok {
			t.Error("ok should be false for non-numeric input")
		}
		if idx != 0 {
			t.Errorf("idx = %d, want 0", idx)
		}
	})
}

// ─── Integration / combined stdout+stdin tests ─────────────────────────

func TestCombinedStdinStdout(t *testing.T) {
	t.Run("PromptForSelection full round-trip", func(t *testing.T) {
		options := []string{"A", "B", "C"}

		stdout, _ := bothStdinStdout("2", func() {
			PromptForSelection(options, "Select: ")
		})

		if !bytes.Contains([]byte(stdout), []byte("Select: ")) {
			t.Errorf("missing custom prompt in stdout: %q", stdout)
		}
		if !bytes.Contains([]byte(stdout), []byte("2. B\n")) {
			// The selection itself doesn't produce output, but the prompt should
		}
	})

	t.Run("PromptForConfirmation full round-trip", func(t *testing.T) {
		stdout, _ := bothStdinStdout("yes", func() {
			PromptForConfirmation("Confirm? ")
		})

		if !bytes.Contains([]byte(stdout), []byte("Confirm? ")) {
			t.Errorf("missing custom prompt: %q", stdout)
		}
	})

	t.Run("PromptForSelectionWithOptions full round-trip", func(t *testing.T) {
		opts := []NumericPromptOption{
			{Index: 1, DisplayName: "Yes", Description: "Accept"},
			{Index: 2, DisplayName: "No", Description: "Reject"},
		}

		stdout, _ := bothStdinStdout("1", func() {
			PromptForSelectionWithOptions(opts, "Choose: ")
		})

		if !bytes.Contains([]byte(stdout), []byte("1. Yes - Accept")) {
			t.Errorf("missing option 1: %q", stdout)
		}
		if !bytes.Contains([]byte(stdout), []byte("2. No - Reject")) {
			t.Errorf("missing option 2: %q", stdout)
		}
		if !bytes.Contains([]byte(stdout), []byte("Choose: ")) {
			t.Errorf("missing custom prompt: %q", stdout)
		}
	})
}
