//go:build js && wasm

package main

import (
	"path"
	"strings"
)

// legacyRelocations maps files stored before the project had its own
// directory — when the repository, the shell's home and its scratch space all
// shared "/" — to their place under the workspace. It only applies on the
// first start after the move, while nothing is stored in the workspace yet,
// and leaves the shell's home and scratch files where they are.
func legacyRelocations(files []IDBFile, workspace, home string) map[string]string {
	within := func(p, dir string) bool {
		dir = strings.TrimSuffix(dir, "/")
		return dir != "" && (p == dir || strings.HasPrefix(p, dir+"/"))
	}
	for _, f := range files {
		if within(f.Path, workspace) {
			return nil
		}
	}
	moves := map[string]string{}
	for _, f := range files {
		p := path.Clean(f.Path)
		if !strings.HasPrefix(p, "/") || within(p, home) || within(p, "/tmp") {
			continue
		}
		moves[f.Path] = path.Join(workspace, p)
	}
	return moves
}
