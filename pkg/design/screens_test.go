package design

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selfContainedScreenHTML is a screen that uses only inline CSS and a
// workspace-relative script, and carries the §9a identity attribute, so it
// yields zero findings.
const selfContainedScreenHTML = `<html data-screen="login">
<head>
  <style>.root { width: 390px; }</style>
  <link rel="stylesheet" href="./styles.css">
</head>
<body>
  <div class="root"><h1>Login</h1><button>Go</button></div>
  <script src="./app.js"></script>
</body>
</html>`

func TestValidateScreen(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		findings := validateScreen("design/screens/login.html", []byte(selfContainedScreenHTML), []Frame{validFrameMobile})
		requireNoWireframeFindings(t, findings)
	})

	t.Run("external-script", func(t *testing.T) {
		content := `<script src="https://cdn.jsdelivr.net/lib.js"></script>`
		findings := validateScreen("design/screens/login.html", []byte(content), nil)
		assert.Equal(t, 1, findingRules(findings)[ruleScreenExternalRef])
	})

	t.Run("protocol-relative-script", func(t *testing.T) {
		content := `<script src="//cdn.example.com/lib.js"></script>`
		findings := validateScreen("design/screens/login.html", []byte(content), nil)
		assert.Equal(t, 1, findingRules(findings)[ruleScreenExternalRef])
	})

	t.Run("external-font-link", func(t *testing.T) {
		content := `<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter">`
		findings := validateScreen("design/screens/login.html", []byte(content), nil)
		assert.Equal(t, 1, findingRules(findings)[ruleScreenExternalRef])
	})

	t.Run("unquoted-external-src", func(t *testing.T) {
		content := `<script src=https://cdn.example.com/lib.js></script>`
		findings := validateScreen("design/screens/login.html", []byte(content), nil)
		assert.Equal(t, 1, findingRules(findings)[ruleScreenExternalRef])
	})

	t.Run("inline-style-external-url", func(t *testing.T) {
		content := `<div style="background: url('https://cdn.example.com/bg.png')"></div>`
		findings := validateScreen("design/screens/login.html", []byte(content), nil)
		assert.Equal(t, 1, findingRules(findings)[ruleScreenExternalRef])
	})

	t.Run("relative-css-ok", func(t *testing.T) {
		content := `<link rel="stylesheet" href="./base.css"><script src="./a.js"></script>`
		findings := validateScreen("design/screens/login.html", []byte(content), nil)
		assert.Equal(t, 0, findingRules(findings)[ruleScreenExternalRef])
	})

	t.Run("external-css-import", func(t *testing.T) {
		content := `<style>@import url("https://fonts.googleapis.com/css"); .a{width:390px;}</style>`
		findings := validateScreen("design/screens/login.html", []byte(content), []Frame{validFrameMobile})
		assert.Equal(t, 1, findingRules(findings)[ruleScreenExternalRef])
	})

	t.Run("bad-slug", func(t *testing.T) {
		findings := validateScreen("design/screens/Login Page.html", []byte(selfContainedScreenHTML), nil)
		assert.Equal(t, 1, findingRules(findings)[ruleScreenSlugName])
	})

	t.Run("device-frame-mismatch", func(t *testing.T) {
		content := `<style>.root{width:777px;}</style><div class="root"></div>`
		findings := validateScreen("design/screens/login.html", []byte(content), []Frame{validFrameMobile})
		assert.Equal(t, 1, findingRules(findings)[ruleScreenDeviceFrame])
		for _, f := range findings {
			if f.Rule == ruleScreenDeviceFrame {
				assert.Equal(t, SeverityInfo, f.Severity)
			}
		}
	})

	t.Run("device-frame-match-ok", func(t *testing.T) {
		content := `<style>.root{width:390px;}</style><div class="root"></div>`
		findings := validateScreen("design/screens/login.html", []byte(content), []Frame{validFrameMobile})
		assert.Equal(t, 0, findingRules(findings)[ruleScreenDeviceFrame])
	})
}

// TestValidateScreenIdentity pins the SP-140-9 §9a identity rules: data-screen
// present on <html> (hard), agreeing with the file stem (hard), and no
// data-status anywhere in the document (hard — status lives in the README
// manifest, one status truth).
func TestValidateScreenIdentity(t *testing.T) {
	t.Run("identity-missing", func(t *testing.T) {
		content := `<!DOCTYPE html><html lang="en"><body>x</body></html>`
		findings := validateScreenIdentity("design/screens/login.html", []byte(content))
		require.Len(t, findings, 1)
		assert.Equal(t, ruleScreenIdentityMissing, findings[0].Rule)
		assert.Equal(t, SeverityError, findings[0].Severity)
		assert.Contains(t, findings[0].Message, `data-screen="login"`)
	})

	t.Run("identity-agrees-clean", func(t *testing.T) {
		content := `<!DOCTYPE html><html lang="en" data-screen="login"><body>x</body></html>`
		findings := validateScreenIdentity("design/screens/login.html", []byte(content))
		assert.Empty(t, findings)
	})

	t.Run("identity-mismatch", func(t *testing.T) {
		content := `<!DOCTYPE html><html lang="en" data-screen="signup"><body>x</body></html>`
		findings := validateScreenIdentity("design/screens/login.html", []byte(content))
		require.Len(t, findings, 1)
		assert.Equal(t, ruleScreenIdentityMismatch, findings[0].Rule)
		assert.Equal(t, SeverityError, findings[0].Severity)
		assert.Contains(t, findings[0].Message, "signup")
		assert.Contains(t, findings[0].Message, "login")
	})

	t.Run("status-attribute-flagged", func(t *testing.T) {
		content := "<!DOCTYPE html>\n<html lang=\"en\" data-screen=\"login\">\n" +
			"<body>\n<div data-screen=\"login\" data-status=\"draft\">x</div>\n</body></html>\n"
		findings := validateScreenIdentity("design/screens/login.html", []byte(content))
		require.Len(t, findings, 1)
		assert.Equal(t, ruleScreenStatusAttr, findings[0].Rule)
		assert.Equal(t, SeverityError, findings[0].Severity)
		assert.Equal(t, 4, findings[0].Line, "the finding anchors to the offending attribute's line")
	})
}

func TestValidateScreensDir(t *testing.T) {
	t.Run("missing-dir", func(t *testing.T) {
		root := t.TempDir()
		findings, err := ValidateScreensDir(root)
		require.NoError(t, err)
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})

	t.Run("clean-with-frames", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "screens"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "screens", "login.html"),
			[]byte(selfContainedScreenHTML), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "README.md"),
			[]byte("frames:\n  mobile: 390x844\n"), 0o644))
		findings, err := ValidateScreensDir(root)
		require.NoError(t, err)
		requireNoWireframeFindings(t, findings)
	})

	t.Run("external-ref-across-files", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "design", "screens"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "design", "screens", "login.html"),
			[]byte(`<script src="https://x.com/a.js"></script>`), 0o644))
		findings, err := ValidateScreensDir(root)
		require.NoError(t, err)
		assert.Equal(t, 1, findingRules(findings)[ruleScreenExternalRef])
	})
}
