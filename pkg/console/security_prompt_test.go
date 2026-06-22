package console

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// TestSecurityPromptHooksRegistered verifies that importing pkg/console
// wires the SelectList-backed pickers into utils.  Without this, the legacy
// "[y/n/a/e]" line prompt would still drive every approval — defeating the
// whole reason for the migration.
func TestSecurityPromptHooksRegistered(t *testing.T) {
	if utils.SecurityPromptHook == nil {
		t.Error("utils.SecurityPromptHook is nil — pkg/console init did not register it")
	}
	if utils.FilesystemSecurityPromptHook == nil {
		t.Error("utils.FilesystemSecurityPromptHook is nil — pkg/console init did not register it")
	}
}

// TestWriteSecurityHeader verifies the header includes the warning glyph,
// the prompt text, and the labeled target on its own indented block.
func TestWriteSecurityHeader(t *testing.T) {
	var buf bytes.Buffer
	writeSecurityHeader(&buf, "High-risk operation — approve to run", "Command", "rm -rf /tmp/foo")

	out := buf.String()
	cases := []string{
		"⚠",                                       // glyph
		"High-risk operation — approve to run",   // prompt
		"Command",                                 // label
		"rm -rf /tmp/foo",                         // target
	}
	for _, want := range cases {
		if !strings.Contains(out, want) {
			t.Errorf("expected header to contain %q, got:\n%s", want, out)
		}
	}
}

// TestWriteSecurityFootnote verifies that the dim footnote line renders the
// supplied caveat text.
func TestWriteSecurityFootnote(t *testing.T) {
	var buf bytes.Buffer
	writeSecurityFootnote(&buf, "Critical ops still block.")
	if !strings.Contains(buf.String(), "Critical ops still block.") {
		t.Errorf("expected footnote text in output, got: %q", buf.String())
	}
}

// TestSecurityApprovalBell verifies that a terminal bell (\a) is emitted
// when a security approval prompt is shown. SP-070-2.
func TestSecurityApprovalBell(t *testing.T) {
	var buf bytes.Buffer
	// The function writes \a + header + footnote, then fails on Run()
	// returning ApprovalChoiceDeny because the buffer is not a TTY.
	choice := askForSecurityApprovalWriter(&buf, "High-risk operation", "rm -rf /tmp/foo")
	if choice != utils.ApprovalChoiceDeny {
		t.Logf("approval choice was %v (expected Deny on non-TTY)", choice)
	}
	out := buf.String()
	// The first byte should be the bell character \a (0x07)
	if len(out) == 0 {
		t.Fatal("expected output, got empty buffer")
	}
	if out[0] != '\a' {
		t.Errorf("expected first byte to be bell (\\a, 0x07), got 0x%02x (%q)", out[0], out[:min(len(out), 20)])
	}
}

// TestFilesystemSecurityApprovalBell verifies that a terminal bell (\a) is
// emitted when a filesystem security approval prompt is shown. SP-070-2.
func TestFilesystemSecurityApprovalBell(t *testing.T) {
	var buf bytes.Buffer
	choice := askForFilesystemSecurityApprovalWriter(&buf, "External path access", "/tmp/foo", "/tmp", utils.FilesystemPromptExternal)
	if choice != utils.ApprovalChoiceDeny {
		t.Logf("approval choice was %v (expected Deny on non-TTY)", choice)
	}
	out := buf.String()
	if len(out) == 0 {
		t.Fatal("expected output, got empty buffer")
	}
	if out[0] != '\a' {
		t.Errorf("expected first byte to be bell (\\a, 0x07), got 0x%02x (%q)", out[0], out[:min(len(out), 20)])
	}
}
