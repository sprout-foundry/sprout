//go:build !js

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// fixturePath turns a slash-rooted Unix fixture path into an absolute
// path on this OS. On Windows the Unix system roots used by the fixtures
// map under %SystemRoot% so they stay system paths; everything else gets
// a drive letter.
func fixturePath(p string) string {
	if runtime.GOOS != "windows" || !strings.HasPrefix(p, "/") {
		return p
	}
	for _, sys := range []string{"/etc", "/System", "/var"} {
		if p == sys || strings.HasPrefix(p, sys+"/") {
			root := os.Getenv("SystemRoot")
			if root == "" {
				root = `C:\Windows`
			}
			return filepath.ToSlash(root) + p
		}
	}
	return "C:" + p
}

var fixtureJSONPath = regexp.MustCompile(`"path": "(/[^"]*)"`)

// fixtureJSON rewrites every "path" value in a workflow JSON fixture
// through fixturePath.
func fixtureJSON(s string) string {
	return fixtureJSONPath.ReplaceAllStringFunc(s, func(m string) string {
		p := fixtureJSONPath.FindStringSubmatch(m)[1]
		return `"path": "` + fixturePath(p) + `"`
	})
}
