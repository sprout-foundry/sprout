package semantic

import (
	"os"
	"os/exec"
	"testing"
)

// requireGopls skips when gopls is not on PATH, unless SPROUT_REQUIRE_GOPLS=1
// is set — the same contract the browser-dependent tests use with
// SPROUT_REQUIRE_BROWSER, so CI asserts the gopls path ran instead of hiding
// behind a green skip. Without the CI side of that contract these tests skip
// everywhere, including CI, and the adapter's gopls-dependent behaviour is
// never exercised.
func requireGopls(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gopls"); err != nil {
		if os.Getenv("SPROUT_REQUIRE_GOPLS") != "" {
			t.Fatalf("gopls is not on PATH, but SPROUT_REQUIRE_GOPLS is set: %v", err)
		}
		t.Skip("gopls not available")
	}
}

// skipIfGoplsInstalled guards the tests that exercise the missing-gopls path.
// They are the mirror of requireGopls: running them on a machine that has
// gopls would assert the wrong branch, so they skip when it is present.
func skipIfGoplsInstalled(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gopls"); err == nil {
		t.Skip("gopls is available; skipping not-available path test")
	}
}
