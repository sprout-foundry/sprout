package design

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWireframeDeprecation pins the SP-140-9 §9a wireframe-tier deprecation:
// every legacy design/wireframes/*.svg earns exactly one informational notice
// naming the SP-140-9 spec section and the primary screen the file should
// become (item 9.4 migrates the tier). The channel is info — not the spec's
// warn — because every finding severity suppresses the webui health strip's
// "validated clean" state, and 9.4's migration window must not hold the
// whole surface in a perpetual advisory state (SP-140-9 §9e steps the
// severity up when the tier is gone).
func TestWireframeDeprecation(t *testing.T) {
	t.Run("one-notice-per-file", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
			"home.svg":  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Home</text></svg>`,
		}, "# draft review ready\n")
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		notices := 0
		for _, f := range findings {
			if f.Rule != ruleWireframeDeprecated {
				continue
			}
			notices++
			assert.Equal(t, SeverityInfo, f.Severity)
			assert.Contains(t, f.Message, "SP-140-9 §9a")
			assert.Contains(t, f.Message, "item 9.4")
		}
		assert.Equal(t, 2, notices, "one notice per legacy wireframe, got %#v", findings)
	})

	t.Run("names-the-primary-screen", func(t *testing.T) {
		f := wireframeDeprecationFinding("design/wireframes/login.svg")
		assert.Contains(t, f.Message, ScreenRelPath("login"))
		assert.Equal(t, "design/wireframes/login.svg", f.File)
	})

	t.Run("no-wireframes-no-notices", func(t *testing.T) {
		root := t.TempDir()
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		assert.Empty(t, findings)
	})
}

// TestScreenRelPath pins the canonical primary-screen path helper the
// deprecation notices and the flow tooling share.
func TestScreenRelPath(t *testing.T) {
	assert.Equal(t, "design/screens/sign-up.html", ScreenRelPath("sign-up"))
}
