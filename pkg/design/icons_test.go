package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validIconSVG is a self-contained icon that satisfies the slug rule.
const validIconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M0 0h24v24H0z" fill="currentColor"/></svg>`

func TestValidateIconSVG(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		findings := validateIconSVG("design/icons/search.svg", []byte(validIconSVG), false)
		requireNoWireframeFindings(t, findings)
	})

	t.Run("script", func(t *testing.T) {
		content := `<svg xmlns="http://www.w3.org/2000/svg"><script>x</script><path d="M0 0h1"/></svg>`
		findings := validateIconSVG("design/icons/search.svg", []byte(content), false)
		assert.Equal(t, 1, findingRules(findings)[ruleIconSelfContainment])
	})

	t.Run("external-ref", func(t *testing.T) {
		content := `<svg xmlns="http://www.w3.org/2000/svg"><image href="https://x.com/a.png"/></svg>`
		findings := validateIconSVG("design/icons/search.svg", []byte(content), false)
		assert.Equal(t, 1, findingRules(findings)[ruleIconSelfContainment])
	})

	t.Run("data-uri-ok", func(t *testing.T) {
		content := `<svg xmlns="http://www.w3.org/2000/svg"><image href="data:image/png;base64,AA=="/></svg>`
		findings := validateIconSVG("design/icons/search.svg", []byte(content), false)
		assert.Equal(t, 0, findingRules(findings)[ruleIconSelfContainment])
	})

	t.Run("bad-slug", func(t *testing.T) {
		findings := validateIconSVG("design/icons/My Icon.svg", []byte(validIconSVG), false)
		assert.Equal(t, 1, findingRules(findings)[ruleIconSlugName])
	})

	t.Run("malformed", func(t *testing.T) {
		findings := validateIconSVG("design/icons/search.svg", []byte("<svg><path"), false)
		assert.Equal(t, 1, findingRules(findings)[ruleIconWellformed])
	})
}

func TestValidateIconSprite(t *testing.T) {
	t.Run("valid-symbols", func(t *testing.T) {
		content := `<svg xmlns="http://www.w3.org/2000/svg"><symbol id="icon-home"><path d="M0 0h1"/></symbol><symbol id="icon-close"><path d="M0 0h1"/></symbol></svg>`
		findings := validateIconSVG("design/icons/sprite.svg", []byte(content), true)
		requireNoWireframeFindings(t, findings)
	})

	t.Run("bad-symbol-id", func(t *testing.T) {
		content := `<svg xmlns="http://www.w3.org/2000/svg"><symbol id="Icon Home"><path d="M0 0h1"/></symbol></svg>`
		findings := validateIconSVG("design/icons/sprite.svg", []byte(content), true)
		assert.Equal(t, 1, findingRules(findings)[ruleIconSpriteSymbol])
		for _, f := range findings {
			if f.Rule == ruleIconSpriteSymbol {
				assert.Equal(t, SeverityError, f.Severity)
			}
		}
	})

	t.Run("empty-sprite", func(t *testing.T) {
		content := `<svg xmlns="http://www.w3.org/2000/svg"><defs/></svg>`
		findings := validateIconSVG("design/icons/sprite.svg", []byte(content), true)
		assert.Equal(t, 1, findingRules(findings)[ruleIconSpriteSymbol])
		for _, f := range findings {
			if f.Rule == ruleIconSpriteSymbol {
				assert.Equal(t, SeverityInfo, f.Severity)
			}
		}
	})
}

func TestValidateIconsDir(t *testing.T) {
	t.Run("missing-dir", func(t *testing.T) {
		root := t.TempDir()
		findings, err := ValidateIconsDir(root)
		require.NoError(t, err)
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})

	t.Run("icon-and-sprite", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "icons"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "icons", "search.svg"), []byte(validIconSVG), 0o644))
		sprite := `<svg xmlns="http://www.w3.org/2000/svg"><symbol id="icon-home"><path d="M0 0h1"/></symbol></svg>`
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "icons", "sprite.svg"), []byte(sprite), 0o644))

		findings, err := ValidateIconsDir(root)
		require.NoError(t, err)
		requireNoWireframeFindings(t, findings)
	})

	t.Run("dangling-bad-icon", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "icons"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "icons", "Bad Name.svg"), []byte(validIconSVG), 0o644))

		findings, err := ValidateIconsDir(root)
		require.NoError(t, err)
		assert.Equal(t, 1, findingRules(findings)[ruleIconSlugName])
	})
}
