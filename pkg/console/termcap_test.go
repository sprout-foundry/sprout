package console

import (
	"bytes"
	"os"
	"testing"
)

// nonFileWriter never satisfies fdWriter, so IsTerminalWriter is false and
// color decisions fall through to the NO_COLOR / FORCE_COLOR checks.
type nonFileWriter struct{ bytes.Buffer }

func TestIsTerminalWriter_NonFileIsNotTerminal(t *testing.T) {
	if IsTerminalWriter(&nonFileWriter{}) {
		t.Error("a writer without a file descriptor must not be a terminal")
	}
}

func TestIsTerminalWriter_NilFileIsNotTerminal(t *testing.T) {
	var f *os.File
	if IsTerminalWriter(f) {
		t.Error("a nil *os.File must not be reported as a terminal")
	}
}

func TestColorEnabled_NoColorWinsOverEverything(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "1")
	if ColorEnabled(&bytes.Buffer{}) {
		t.Error("NO_COLOR must disable color even when FORCE_COLOR is set")
	}
}

func TestColorEnabled_ForceColorEnablesAnyWriter(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	if !ColorEnabled(&bytes.Buffer{}) {
		t.Error("FORCE_COLOR must enable color for non-terminal writers")
	}
}

func TestColorEnabled_DumbTerminalSuppresses(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("TERM", "dumb")
	if ColorEnabled(&bytes.Buffer{}) {
		t.Error("TERM=dumb must suppress color")
	}
}

func TestSGR_GatesOnWriter(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	if got := SGR(&bytes.Buffer{}, ColorBold); got != "" {
		t.Errorf("SGR on a non-terminal writer = %q, want empty", got)
	}

	t.Setenv("FORCE_COLOR", "1")
	if got := SGR(&bytes.Buffer{}, ColorBold); got != ColorBold {
		t.Errorf("SGR with FORCE_COLOR = %q, want %q", got, ColorBold)
	}
}

func TestColorOn_ForceColorWins(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	if !ColorOn() {
		t.Error("ColorOn must honor FORCE_COLOR")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorOn() {
		t.Error("ColorOn must honor NO_COLOR")
	}
}

func TestSupportsCursorControl_NotAFakeFromForceColor(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	// FORCE_COLOR fakes color, never cursor control.
	if SupportsCursorControl(&bytes.Buffer{}) {
		t.Error("FORCE_COLOR must not fake cursor control on a non-terminal writer")
	}
}

func TestSupportsCursorControl_DumbTerminalSuppresses(t *testing.T) {
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if SupportsCursorControl(os.Stderr) {
		t.Error("TERM=dumb must suppress cursor control")
	}
}
