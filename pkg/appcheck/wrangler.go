//go:build !js

package appcheck

import (
	"os"
	"path/filepath"
)

// wranglerBin returns the project's wrangler binary, or "" when it has none
// (a project without wrangler is served statically, with a note in the
// report when it has a server).
func wranglerBin(root string) string {
	local := filepath.Join(root, "node_modules", ".bin", "wrangler")
	if info, err := os.Stat(local); err == nil && !info.IsDir() {
		return local
	}
	return ""
}
