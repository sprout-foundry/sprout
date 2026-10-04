package agent

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// truncateString is used by buildDisplayName (tool-call summaries); these
// pin its UTF-8 safety after the rune-based rewrite.
func TestTruncateString_MultibyteSafe(t *testing.T) {
	t.Parallel()

	s := strings.Repeat("é", 100) // 2 bytes per rune, 200 bytes total
	got := truncateString(s, 77)
	require.Equal(t, 80, len([]rune(got))) // 77 runes + the 3-rune ellipsis
	require.True(t, utf8.ValidString(got))
	require.True(t, strings.HasSuffix(got, "..."))

	// At or under the limit: passthrough, no ellipsis.
	require.Equal(t, s, truncateString(s, 100))
	require.Equal(t, s, truncateString(s, 500))
}

func TestBuildDisplayName_MultibyteToolArgs(t *testing.T) {
	t.Parallel()

	// A web_search query long enough to truncate, made of multi-byte runes.
	query := strings.Repeat("引擎", 60) // 120 runes, 360 bytes
	got := buildDisplayName("web_search", map[string]interface{}{"query": query})
	require.True(t, utf8.ValidString(got))
	require.Contains(t, got, "web_search")
	// Exactly one ellipsis — the format string owns it, the truncator must
	// not add a second (regression pin for the double-ellipsis bug).
	require.True(t, strings.HasSuffix(got, "..."))
	require.False(t, strings.HasSuffix(got, "......"))

	// ASCII behavior is unchanged from the byte-slice era: 77 chars + "...".
	longCmd := strings.Repeat("a", 100)
	got = buildDisplayName("shell_command", map[string]interface{}{"command": longCmd})
	require.Equal(t, "shell_command "+strings.Repeat("a", 77)+"...", got)
}
