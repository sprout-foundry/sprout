package cliui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatAgentNotice_IndentsAndWrapsUnderTheGlyph(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	msg := "Tool 'shell_command' failed (transient): standalone sleep/wait is not supported as a tool call, use check_background instead"
	got := formatAgentNotice("tool_error", msg, 50)
	rows := strings.Split(got, "\n")
	require.Greater(t, len(rows), 1, "a long error wraps")
	require.True(t, strings.HasPrefix(rows[0], "  ✗ Tool"), rows[0])
	for _, row := range rows[1:] {
		require.True(t, strings.HasPrefix(row, "    ") && row[4] != ' ', "continuation lines up under the text: %q", row)
		require.LessOrEqual(t, len([]rune(row)), 50)
	}
}

func TestFormatAgentNotice_SecurityCautionDropsTheDuplicateTag(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := formatAgentNotice("security_caution", "[Security] user rejected shell_command", 120)
	require.Equal(t, "  ⚠ [SECURITY CAUTION] user rejected shell_command", got)
}
