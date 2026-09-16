package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBrandFile(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		content := "# Brand\n\nPrimary is {color.brand.primary} and accent is {color.brand.secondary}.\n"
		findings := ValidateBrandFile("design/brand/brand.md", []byte(content))
		requireNoWireframeFindings(t, findings)
	})

	t.Run("raw-hex", func(t *testing.T) {
		content := "Primary is #ff0000 and accent is {color.brand.secondary}.\n"
		findings := ValidateBrandFile("design/brand/brand.md", []byte(content))
		assert.Equal(t, 1, findingRules(findings)[ruleBrandRawHex])
		for _, f := range findings {
			if f.Rule == ruleBrandRawHex {
				assert.Equal(t, SeverityWarn, f.Severity)
				assert.Contains(t, f.Message, "#ff0000")
			}
		}
	})

	t.Run("raw-hex-lengths", func(t *testing.T) {
		for _, v := range []string{"#f00", "#f00a", "#0a0a0a", "#0a0a0a00"} {
			findings := ValidateBrandFile("design/brand/brand.md", []byte("x "+v+" y\n"))
			var hexFinding *Finding
			for i := range findings {
				if findings[i].Rule == ruleBrandRawHex {
					hexFinding = &findings[i]
				}
			}
			require.NotNil(t, hexFinding, "value %s should produce a raw-hex finding", v)
			assert.Contains(t, hexFinding.Message, v)
			assert.Equal(t, 1, findingRules(findings)[ruleBrandRawHex])
		}
	})

	t.Run("invalid-lengths-ignored", func(t *testing.T) {
		// 5- or 7-digit runs are not valid CSS hex colors, so they are not
		// flagged (avoids a partial-prefix match like #1234 of #12345).
		for _, v := range []string{"#12345", "#ff00000"} {
			findings := ValidateBrandFile("design/brand/brand.md", []byte("x {a.b} "+v+" y\n"))
			assert.Equal(t, 0, findingRules(findings)[ruleBrandRawHex], "value %s should not be flagged", v)
		}
	})

	t.Run("markdown-headers-not-raw-hex", func(t *testing.T) {
		content := "# Brand\n## Voice\n## Palette\n\nThe logo is {color.brand.primary}.\n"
		findings := ValidateBrandFile("design/brand/brand.md", []byte(content))
		assert.Equal(t, 0, findingRules(findings)[ruleBrandRawHex], "markdown headers must not be treated as raw hex")
		assert.Equal(t, 0, findingRules(findings)[ruleBrandNoTokenRefs])
	})

	t.Run("no-token-refs", func(t *testing.T) {
		content := "We sound friendly and approachable.\n"
		findings := ValidateBrandFile("design/brand/brand.md", []byte(content))
		assert.Equal(t, 1, findingRules(findings)[ruleBrandNoTokenRefs])
		assert.Equal(t, 0, findingRules(findings)[ruleBrandRawHex])
	})
}

func TestValidateBrandDir(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		root := t.TempDir()
		assert.Empty(t, ValidateBrandDir(root))
	})

	t.Run("present", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "brand"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "brand", "brand.md"),
			[]byte("Logo in {color.brand.primary}.\n"), 0o644))
		assert.Empty(t, ValidateBrandDir(root))
	})

	t.Run("raw-hex", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "brand"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "brand", "brand.md"),
			[]byte("Logo in #00ff88.\n"), 0o644))
		findings := ValidateBrandDir(root)
		assert.Equal(t, 1, findingRules(findings)[ruleBrandRawHex])
	})
}
