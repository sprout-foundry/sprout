package injection

import "testing"

func TestClassifyPath_KnownMarkers(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"node_modules at root", "node_modules/left-pad/index.js"},
		{"node_modules nested", "webui/node_modules/react/index.js"},
		{"vendor", "vendor/github.com/acme/lib/lib.go"},
		{"venv", ".venv/lib/python3.11/site-packages/x.py"},
		{"venv plain", "venv/bin/activate"},
		{"site-packages", "site-packages/requests/api.py"},
		{"m2", ".m2/repository/org/foo/foo.jar"},
		{"cargo", ".cargo/registry/src/crate/lib.rs"},
		{"git dir", ".git/config"},
		{"marker is the whole path", "vendor"},
		{"leading dot-slash", "./node_modules/x.js"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A decision on the root saying "ours" must NOT defeat a marker.
			decisions := map[string]bool{".": false}
			if got := ClassifyPath(tc.path, decisions); got != TrustThirdParty {
				t.Errorf("ClassifyPath(%q) = %v, want TrustThirdParty", tc.path, got)
			}
		})
	}
}

func TestClassifyPath_MarkerIsWholeSegment(t *testing.T) {
	// A directory that merely contains a marker name as a substring is not
	// a dependency tree.
	for _, path := range []string{"vendors/acme/lib.go", "my_node_modules/x.js", "git/config"} {
		if got := ClassifyPath(path, nil); got != TrustUnknown {
			t.Errorf("ClassifyPath(%q) = %v, want TrustUnknown (marker must be a whole segment)", path, got)
		}
	}
}

func TestClassifyPath_LongestPrefixWins(t *testing.T) {
	decisions := map[string]bool{
		".":                  false, // repo root is ours
		"deps":               true,  // except deps/ ...
		"deps/internal-fork": false, // ... apart from this adopted subtree
	}
	cases := []struct {
		path string
		want TrustClass
	}{
		{"main.go", TrustFirstParty},                 // root decision
		{"cmd/trust.go", TrustFirstParty},            // root decision
		{"./cmd/trust.go", TrustFirstParty},          // normalized
		{"deps", TrustThirdParty},                    // exact match
		{"deps/acme/lib.go", TrustThirdParty},        // shorter prefix
		{"deps/internal-fork/x.go", TrustFirstParty}, // longer prefix overrides
		{"deps/internal-fork", TrustFirstParty},      // exact longer match
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := ClassifyPath(tc.path, decisions); got != tc.want {
				t.Errorf("ClassifyPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestClassifyPath_PrefixIsSegmentWise is the non-match case the spec calls
// out: a decision on "third_party/acme" is a statement about that directory,
// not about every sibling whose name happens to start with the same bytes.
// (The example uses a non-marker path, because a marker segment such as
// "vendor" is third-party by rule 1 whatever the decisions say.)
func TestClassifyPath_PrefixIsSegmentWise(t *testing.T) {
	decisions := map[string]bool{"third_party/acme": true}
	check := func(path string, want TrustClass) {
		t.Helper()
		if got := ClassifyPath(path, decisions); got != want {
			t.Errorf("ClassifyPath(%q) = %v, want %v", path, got, want)
		}
	}
	check("third_party/acme/lib.go", TrustThirdParty)
	check("third_party/acmecorp/lib.go", TrustUnknown)
	check("third_party/acme-extra/lib.go", TrustUnknown)
}

func TestClassifyPath_UnknownWhenNoDecision(t *testing.T) {
	if got := ClassifyPath("src/main.go", nil); got != TrustUnknown {
		t.Errorf("nil decisions = %v, want TrustUnknown", got)
	}
	if got := ClassifyPath("src/main.go", map[string]bool{}); got != TrustUnknown {
		t.Errorf("empty decisions = %v, want TrustUnknown", got)
	}
	// An unrelated decision must not leak onto this path.
	if got := ClassifyPath("src/main.go", map[string]bool{"other/dir": true}); got != TrustUnknown {
		t.Errorf("unrelated decision = %v, want TrustUnknown", got)
	}
}

func TestClassifyPath_FirstPartyDecision(t *testing.T) {
	decisions := map[string]bool{"cloned-repo": false}
	if got := ClassifyPath("cloned-repo/main.go", decisions); got != TrustFirstParty {
		t.Errorf("= %v, want TrustFirstParty", got)
	}
}

func TestNormalizeRelPath(t *testing.T) {
	cases := map[string]string{
		"":        ".",
		".":       ".",
		"./":      ".",
		"./a/b":   "a/b",
		"././a":   "a",
		"a/b/":    "a/b",
		"/a/b":    "a/b",
		`a\b`:     "a/b",
		"  a/b  ": "a/b",
		"a/./b":   "a/./b", // not cleaned: only the leading "./" is trimmed
	}
	for in, want := range cases {
		if got := NormalizeRelPath(in); got != want {
			t.Errorf("NormalizeRelPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrustClassString(t *testing.T) {
	cases := map[TrustClass]string{
		TrustUnknown:    "unknown",
		TrustFirstParty: "first-party",
		TrustThirdParty: "third-party",
	}
	for class, want := range cases {
		if got := class.String(); got != want {
			t.Errorf("TrustClass(%d).String() = %q, want %q", int(class), got, want)
		}
	}
}

// TestTrustClassZeroValueIsUnknown pins the invariant the package doc states:
// the zero value must never read as an approval or as "our own code".
func TestTrustClassZeroValueIsUnknown(t *testing.T) {
	var zero TrustClass
	if zero != TrustUnknown {
		t.Fatalf("zero TrustClass = %v, want TrustUnknown", zero)
	}
}
