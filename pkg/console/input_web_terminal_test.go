package console

import (
	"bytes"
	"strings"
	"testing"
)

// TestWriteModifyOtherKeys_DisabledInWebTerminal verifies that the
// CSI > 4 ; 1 m enable / CSI > 4 ; 0 m disable sequences are skipped when
// SPROUT_WEB_TERMINAL or SPROUT_WEB_TERMINAL is set, and emitted otherwise.
//
// xterm.js (which backs the sprout webui terminal) does not implement the
// modifyOtherKeys / CSI u keyboard protocol. Emitting the enable sequence
// has no effect on it, but more importantly modified keystrokes
// (Shift+Enter, Ctrl+Arrow, etc.) then never reach sprout's input parser.
// The legacy xterm escape parser in input_escape_parser.go still decodes
// those keys via the conventional ESC[1;Nm encodings, so disabling the
// protocol here is a graceful degradation rather than a feature loss.
func TestWriteModifyOtherKeys_DisabledInWebTerminal(t *testing.T) {
	tests := []struct {
		name      string
		envVars   map[string]string
		wantEmpty bool
	}{
		{
			name:      "no env vars → emit (real terminal)",
			envVars:   map[string]string{},
			wantEmpty: false,
		},
		{
			name:      "SPROUT_WEB_TERMINAL=1 → skip",
			envVars:   map[string]string{"SPROUT_WEB_TERMINAL": "1"},
			wantEmpty: true,
		},
		{
			name:      "SPROUT_WEB_TERMINAL=1 (legacy) → skip",
			envVars:   map[string]string{"SPROUT_WEB_TERMINAL": "1"},
			wantEmpty: true,
		},
		{
			name:      "SPROUT_WEB_TERMINAL=\"\" treated as unset → emit",
			envVars:   map[string]string{"SPROUT_WEB_TERMINAL": ""},
			wantEmpty: false,
		},
		{
			name:      "SPROUT_WEB_TERMINAL=\"\" treated as unset → emit",
			envVars:   map[string]string{"SPROUT_WEB_TERMINAL": ""},
			wantEmpty: false,
		},
		{
			name:      "SPROUT_WEB_TERMINAL=1 → skip",
			envVars:   map[string]string{"SPROUT_WEB_TERMINAL": "1"},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv unsets on test cleanup, but we must clear any
			// inherited value first so the empty-string test case
			// exercises the true "unset" path.
			t.Setenv("SPROUT_WEB_TERMINAL", "")
			t.Setenv("SPROUT_WEB_TERMINAL", "")
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			var buf bytes.Buffer
			writeModifyOtherKeysEnable(&buf)
			got := buf.String()
			isEmpty := got == ""

			if isEmpty != tt.wantEmpty {
				t.Errorf("writeModifyOtherKeysEnable() = %q; wantEmpty=%v", got, tt.wantEmpty)
			}
			if !isEmpty && !strings.Contains(got, "\033[>4;") {
				t.Errorf("writeModifyOtherKeysEnable() = %q; expected CSI > 4 sequence", got)
			}

			buf.Reset()
			writeModifyOtherKeysDisable(&buf)
			got = buf.String()
			isEmpty = got == ""
			if isEmpty != tt.wantEmpty {
				t.Errorf("writeModifyOtherKeysDisable() = %q; wantEmpty=%v", got, tt.wantEmpty)
			}
			if !isEmpty && !strings.Contains(got, "\033[>4;") {
				t.Errorf("writeModifyOtherKeysDisable() = %q; expected CSI > 4 sequence", got)
			}
		})
	}
}
