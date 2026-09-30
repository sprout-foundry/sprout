package automate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// fixtureAbs maps a Unix-style absolute fixture path onto an absolute path
// for the host OS. On Windows "/etc/..." fixtures land under %SystemRoot% so
// they still exercise the system-prefix warning.
func fixtureAbs(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, "/etc/"); ok {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return filepath.Join(root, filepath.FromSlash(rest))
	}
	return `C:` + filepath.FromSlash(p)
}

func fixtureJSONPath(p string) string {
	b, err := json.Marshal(fixtureAbs(p))
	if err != nil {
		panic(err)
	}
	return string(b)
}
