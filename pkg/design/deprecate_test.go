package design

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWireframeDeprecation pins the SP-140-9 §9a wireframe-tier deprecation
// at its post-9.4 severity: every legacy design/wireframes/*.svg earns
// exactly one ERROR naming the §9a section and the primary screen the file
// must become. Item 9.4 migrated the tier, so the transitional info window
// is closed — presence is the error (the spec's "warn window = one release,
// then reject" cutover), while flow_mmd_legacy stays info for external
// trees' still-unmigrated flows.
func TestWireframeDeprecation(t *testing.T) {
	t.Run("one-error-per-file", func(t *testing.T) {
		root := t.TempDir()
		writeWireframeTree(t, root, map[string]string{
			"login.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Login</text></svg>`,
			"home.svg":  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text>Home</text></svg>`,
		}, "# draft review ready\n")
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		deprecations := 0
		for _, f := range findings {
			if f.Rule != ruleWireframeDeprecated {
				continue
			}
			deprecations++
			assert.Equal(t, SeverityError, f.Severity)
			assert.Contains(t, f.Message, "SP-140-9 §9a")
			assert.Contains(t, f.Message, "item 9.4")
		}
		assert.Equal(t, 2, deprecations, "one error per legacy wireframe, got %#v", findings)
	})

	t.Run("names-the-primary-screen", func(t *testing.T) {
		f := wireframeDeprecationFinding("design/wireframes/login.svg")
		assert.Contains(t, f.Message, ScreenRelPath("login"))
		assert.Equal(t, "design/wireframes/login.svg", f.File)
	})

	t.Run("no-wireframes-no-findings", func(t *testing.T) {
		root := t.TempDir()
		findings, err := ValidateWireframesDir(root)
		require.NoError(t, err)
		assert.Empty(t, findings)
	})
}

// TestScreenRelPath pins the canonical primary-screen path helper the
// deprecation findings and the flow tooling share.
func TestScreenRelPath(t *testing.T) {
	assert.Equal(t, "design/screens/sign-up.html", ScreenRelPath("sign-up"))
}
