//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

var automateSessionDir string // --dir on status/logs/stop: explicit .sprout session root

// discoverSproutSessionRoot resolves the root whose automate/ subdir holds
// session records (.sprout/automate/<id>.json, or <state dir>/automate for
// the central registry). Order: nearest ancestor of startDir (inclusive)
// containing a .sprout/automate directory; then the central registry
// (<state dir>, i.e. ~/.local/state/sprout by default) if its automate/
// subdir exists; then <startDir>/.sprout so writers (run) create it where
// the user stands, matching pre-AUTOM-2 behavior.
func discoverSproutSessionRoot(startDir string) string {
	dir := filepath.Clean(startDir)
	for depth := 0; depth < maxGitWalkDepth; depth++ {
		if info, err := os.Stat(filepath.Join(dir, ".sprout", "automate")); err == nil && info.IsDir() {
			return filepath.Join(dir, ".sprout")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if root, ok := centralAutomateRegistryRoot(); ok {
		return root
	}
	return filepath.Join(startDir, ".sprout")
}

// centralAutomateRegistryRoot returns the sprout state dir when it already
// contains an automate/ subdir — read-only resolution so a missing registry
// is never materialized by a status/logs/stop lookup.
func centralAutomateRegistryRoot() (string, bool) {
	p, err := envutil.StateDirPath()
	if err != nil {
		return "", false
	}
	info, err := os.Stat(filepath.Join(p, "automate"))
	if err != nil || !info.IsDir() {
		return "", false
	}
	return p, true
}

func automateSessionRoot() (string, error) {
	if automateSessionDir != "" {
		abs, err := filepath.Abs(automateSessionDir)
		if err != nil {
			return "", fmt.Errorf("resolve --dir: %w", err)
		}
		return abs, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return discoverSproutSessionRoot(cwd), nil
}
