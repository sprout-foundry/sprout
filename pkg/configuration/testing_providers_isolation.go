package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type providerFileStamp struct {
	modTime time.Time
	size    int64
}

func snapshotProvidersDir(dir string) map[string]providerFileStamp {
	stamps := map[string]providerFileStamp{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return stamps
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info.IsDir() {
			continue
		}
		stamps[e.Name()] = providerFileStamp{modTime: info.ModTime(), size: info.Size()}
	}
	return stamps
}

// providerLeaks lists files added or changed in dir since before.
func providerLeaks(dir string, before map[string]providerFileStamp) []string {
	var leaked []string
	for name, now := range snapshotProvidersDir(dir) {
		was, existed := before[name]
		if !existed || !was.modTime.Equal(now.modTime) || was.size != now.size {
			leaked = append(leaked, name)
		}
	}
	sort.Strings(leaked)
	return leaked
}

// IsolateGlobalConfigForTests is for a test binary's TestMain. Custom
// providers always live in the user-global config dir (HOME/XDG-based,
// ignoring SPROUT_CONFIG), so a test that saves one without the per-test
// helpers wrote it into the developer's real ~/.config/sprout/providers. This
// points XDG_CONFIG_HOME at a temp dir for the whole run and returns finish,
// which restores the environment and fails the run (exit code 1) if anything
// was still added to or changed in the real providers dir.
//
//	func TestMain(m *testing.M) {
//		finish := configuration.IsolateGlobalConfigForTests()
//		os.Exit(finish(m.Run()))
//	}
func IsolateGlobalConfigForTests() (finish func(code int) int) {
	realDir := ""
	if dir, err := getDefaultConfigDir(); err == nil {
		realDir = filepath.Join(dir, ProvidersDirName)
	}
	before := snapshotProvidersDir(realDir)

	prevXDG, hadXDG := os.LookupEnv("XDG_CONFIG_HOME")
	tmp, err := os.MkdirTemp("", "sprout-test-xdg-config-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "IsolateGlobalConfigForTests: create temp config dir: %v\n", err)
		os.Exit(1)
	}
	_ = os.Setenv("XDG_CONFIG_HOME", tmp)

	return func(code int) int {
		if hadXDG {
			_ = os.Setenv("XDG_CONFIG_HOME", prevXDG)
		} else {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		}
		_ = os.RemoveAll(tmp)
		if realDir == "" {
			return code
		}
		leaked := providerLeaks(realDir, before)
		if len(leaked) == 0 {
			return code
		}
		fmt.Fprintf(os.Stderr,
			"[provider-leak] %d file(s) added or changed in the real providers dir %q during the test run: %v\n"+
				"  A test saved a custom provider outside the test isolation. Remove them, and route the test through "+
				"configuration.NewTestManager or a temp XDG_CONFIG_HOME/HOME.\n"+
				"  (Custom providers change only on user action, so a sprout running alongside doesn't explain this.)\n",
			len(leaked), realDir, leaked)
		if code == 0 {
			return 1
		}
		return code
	}
}
