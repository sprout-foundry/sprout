package automate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func summarySystemPathPrefixList() []string {
	if runtime.GOOS == "windows" {
		return windowsSummarySystemPathPrefixes()
	}
	return []string{
		"/etc",
		"/usr",
		"/var",
		"/bin",
		"/sbin",
		"/boot",
		"/proc",
		"/sys",
		"/dev",
		"/lib",
		"/lib64",
		"/opt",
		"/root",
		"/System",
		"/Library",
		"/private/etc",
		"/private/var",
		"/Applications",
	}
}

// windowsSummarySystemPathPrefixes mirrors pkg/agent/path_tier.go: install
// roots come from the environment (Windows need not live on C:) and are
// lowercased because Windows paths compare case-insensitively.
func windowsSummarySystemPathPrefixes() []string {
	defaults := [][2]string{
		{"SystemRoot", `C:\Windows`},
		{"ProgramFiles", `C:\Program Files`},
		{"ProgramFiles(x86)", `C:\Program Files (x86)`},
		{"ProgramData", `C:\ProgramData`},
	}
	prefixes := make([]string, 0, len(defaults))
	for _, d := range defaults {
		dir := os.Getenv(d[0])
		if dir == "" {
			dir = d[1]
		}
		prefixes = append(prefixes, strings.ToLower(filepath.Clean(dir)))
	}
	return prefixes
}
