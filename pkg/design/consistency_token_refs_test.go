package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateArtifactTokenRefs (SP-140-1 §1a/§1e): a {group.token} reference in
// a wireframe or component must resolve against the tree's DTCG leaves. The
// canonical failure is the missing self-nesting tier group — a color file
// with bare "dark"/"light" groups — which parses cleanly, exports cleanly,
// and silently reports every {color.*} ref as unknown downstream.

// writeTokenRefTree seeds a design tree with one wireframe and a token file.
func writeTokenRefTree(t *testing.T, root string, tokenJSON string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "wireframes"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "tokens"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "design", "tokens", "color.tokens.json"), []byte(tokenJSON), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "design", "README.md"),
		[]byte("# Design\n\nframes:\n  desktop: 1440x900\n"), 0o644))
}

func TestValidateArtifactTokenRefs(t *testing.T) {
	const wireframe = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1440 900">` +
		`<!-- {color.dark.bg.primary} --><!-- {color.dark.text.primary} --><text>Login</text></svg>`

	t.Run("self-nested-tier-resolves-clean", func(t *testing.T) {
		root := t.TempDir()
		writeTokenRefTree(t, root, `{"color":{"dark":{"bg":{"primary":{"$value":"#282c34","$type":"color"}},"text":{"primary":{"$value":"#abb2bf","$type":"color"}}}}}`)
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "wireframes", "login.svg"), []byte(wireframe), 0o644))
		findings := ValidateConsistency(root)
		assert.Equal(t, 0, findingRules(findings)[ruleConsistencyTokenRefDangling], "got %#v", findings)
	})

	t.Run("missing-tier-group-dangling-refs", func(t *testing.T) {
		root := t.TempDir()
		// Bare "dark" top level — the exact shape that broke the dogfood
		// tree: parses fine, but {color.*} refs cannot resolve.
		writeTokenRefTree(t, root, `{"dark":{"bg":{"primary":{"$value":"#282c34","$type":"color"}},"text":{"primary":{"$value":"#abb2bf","$type":"color"}}}}`)
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "wireframes", "login.svg"), []byte(wireframe), 0o644))
		findings := ValidateConsistency(root)
		rules := findingRules(findings)
		assert.Equal(t, 2, rules[ruleConsistencyTokenRefDangling], "both refs dangle: got %#v", findings)
		for _, f := range findings {
			if f.Rule == ruleConsistencyTokenRefDangling {
				assert.Equal(t, SeverityWarn, f.Severity)
				assert.Contains(t, f.Message, "self-nesting group")
				assert.Equal(t, "design/wireframes/login.svg", f.File)
			}
		}
	})

	t.Run("components-covered-too", func(t *testing.T) {
		root := t.TempDir()
		writeTokenRefTree(t, root, `{"color":{"dark":{"bg":{"primary":{"$value":"#282c34","$type":"color"}}}}}`)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "components"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "components", "panel.svg"),
			[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 480 120"><!-- {color.dark.text.primary} --><text>Panel</text></svg>`), 0o644))
		findings := ValidateConsistency(root)
		assert.Equal(t, 1, findingRules(findings)[ruleConsistencyTokenRefDangling], "got %#v", findings)
	})

	t.Run("no-tokens-yields-no-findings", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "wireframes"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "wireframes", "login.svg"), []byte(wireframe), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "README.md"),
			[]byte("# Design\n\nframes:\n  desktop: 1440x900\n"), 0o644))
		findings := ValidateConsistency(root)
		assert.Equal(t, 0, findingRules(findings)[ruleConsistencyTokenRefDangling], "got %#v", findings)
	})
}
