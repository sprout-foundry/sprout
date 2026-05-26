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
