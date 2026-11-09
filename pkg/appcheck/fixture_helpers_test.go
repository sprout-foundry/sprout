//go:build !js

package appcheck

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// writeEmptyFile creates a file, standing in for a screenshot.
func writeEmptyFile(path string) error {
	return os.WriteFile(path, []byte("png"), 0o644)
}

// containsText reports whether any of values contains want.
func containsText(values []string, want string) bool {
	for _, v := range values {
		if strings.Contains(v, want) {
			return true
		}
	}
	return false
}

// writeFixtureProject writes a minimal project the check accepts: a
// package.json and a build output, so a run reaches the browser and stops
// there when no browser is available.
func writeFixtureProject(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"package.json":    `{"name":"fixture","scripts":{"build":"true"}}`,
		"dist/index.html": "<!doctype html><title>Fixture</title><h1>Fixture</h1>",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

// unavailableBrowser is a RodBrowser that reports no browser, the way a
// machine without Chromium does.
type unavailableBrowser struct{}

// OpenSession implements RodBrowser.
func (unavailableBrowser) OpenSession(context.Context) (RodSession, error) {
	return nil, fmt.Errorf("%w: no Chrome or Chromium found", webcontent.ErrBrowserUnavailable)
}
