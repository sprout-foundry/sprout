// turn_application_code_test.go — the application-code predicate and the
// filtered turn window it backs: a docs-only or .sprout-bookkeeping turn
// must read as "no code changes" (the verification gate stays closed),
// and a turn that touched one application-code file alongside docs must
// still read as a code turn.

package agent

import "testing"

// trackWrite and trackEdit record changes and fail the test if the
// tracker refuses one (mirroring change_tracking_turn_test.go's helpers).
func trackWrite(t *testing.T, tracker *ChangeTracker, path, original, newContent string, existed bool) {
	t.Helper()
	if err := tracker.TrackFileWriteState(path, original, newContent, existed); err != nil {
		t.Fatalf("TrackFileWriteState(%q): %v", path, err)
	}
}

func trackEdit(t *testing.T, tracker *ChangeTracker, path, original, newContent string) {
	t.Helper()
	if err := tracker.TrackFileEdit(path, original, newContent); err != nil {
		t.Fatalf("TrackFileEdit(%q): %v", path, err)
	}
}

func TestIsApplicationCodePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		// Documentation: markdown anywhere.
		{name: "readme", path: "README.md", want: false},
		{name: "changelog", path: "CHANGELOG.md", want: false},
		{name: "markdown nested", path: "notes/design.md", want: false},
		{name: "markdown uppercase extension", path: "guide.MD", want: false},

		// Documentation: anything under a docs directory segment.
		{name: "docs dir", path: "docs/guide.md", want: false},
		{name: "docs dir, any file kind", path: "docs/diagram.svg", want: false},
		{name: "docs dir nested", path: "foo/docs/x.md", want: false},
		{name: "docs dir uppercase", path: "project/Docs/readme.txt", want: false},
		{name: "docs dir absolute", path: "/ws/docs/api.md", want: false},

		// Sprout bookkeeping: a .sprout path segment.
		{name: "sprout plan", path: ".sprout/plan.json", want: false},
		{name: "sprout starter", path: ".sprout/starter.json", want: false},
		{name: "sprout nested", path: "project/.sprout/plan.json", want: false},
		{name: "sprout absolute", path: "/ws/.sprout/sessions/x.json", want: false},

		// Application code: sources and build configuration.
		{name: "go source", path: "src/app.go", want: true},
		{name: "pkg go source", path: "pkg/x/y.go", want: true},
		{name: "typescript", path: "webui/src/App.tsx", want: true},
		{name: "javascript", path: "src/app.js", want: true},
		{name: "python", path: "scripts/main.py", want: true},
		{name: "rust", path: "src/main.rs", want: true},
		{name: "json config", path: "package.json", want: true},
		{name: "go.mod", path: "go.mod", want: true},
		{name: "css", path: "webui/src/app.css", want: true},
		{name: "html", path: "webui/index.html", want: true},
		{name: "makefile", path: "Makefile", want: true},
		{name: "no extension file named docs", path: "pkg/docs", want: true},
		{name: "yaml config", path: ".github/workflows/ci.yml", want: true},

		// Look-alikes that must NOT be filtered.
		{name: "sprout substring segment", path: "mysprout/plan.json", want: true},
		{name: "sprout suffix segment", path: "x.sprout/data.json", want: true},
		{name: "docs substring segment", path: "docsify/app.js", want: true},
		{name: "md extension is whole suffix", path: "src/main.mdx", want: true},
		{name: "code under a docs-named repo prefix", path: "docs-utils/read.go", want: true},

		// Degenerate input: an empty path is not a file at all.
		{name: "empty", path: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsApplicationCodePath(tt.path); got != tt.want {
				t.Errorf("IsApplicationCodePath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestTurnChangedApplicationPaths pins the filtered window over a real
// tracker: docs and .sprout paths drop out, order of the survivors keeps
// the first-seen order, and an all-docs window reads as nil (the gate's
// zero case).
func TestTurnChangedApplicationPaths(t *testing.T) {
	ag := NewTestAgent()
	tracker := NewChangeTracker(nil, "app-paths")
	tracker.MarkTurnStart()
	trackWrite(t, tracker, "/ws/.sprout/plan.json", "old", "new", true)
	trackEdit(t, tracker, "/ws/README.md", "old", "new")
	trackWrite(t, tracker, "/ws/src/app.go", "old", "new", true)
	trackEdit(t, tracker, "/ws/docs/guide.md", "old", "new")
	trackWrite(t, tracker, "/ws/main.go", "", "new", false)
	ag.changeTracker = tracker

	// The raw window still sees everything (its own contract, pinned
	// elsewhere).
	if got := len(ag.TurnChangedPaths()); got != 5 {
		t.Fatalf("TurnChangedPaths = %d paths, want 5 (the raw window is unfiltered)", got)
	}

	got := ag.TurnChangedApplicationPaths()
	want := []string{"/ws/src/app.go", "/ws/main.go"}
	if len(got) != len(want) {
		t.Fatalf("TurnChangedApplicationPaths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("TurnChangedApplicationPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestTurnChangedApplicationPaths_EmptyCases pins the nil cases the gate
// reads as zero: no tracker, a tracker with an empty window, and a
// window holding only docs and .sprout bookkeeping.
func TestTurnChangedApplicationPaths_EmptyCases(t *testing.T) {
	// No tracker at all.
	agNone := NewTestAgent()
	if got := agNone.TurnChangedApplicationPaths(); got != nil {
		t.Errorf("no-tracker window = %v, want nil", got)
	}

	// A tracker whose turn window opened and recorded nothing.
	agEmpty := NewTestAgent()
	tracker := NewChangeTracker(nil, "app-paths-empty")
	tracker.MarkTurnStart()
	agEmpty.changeTracker = tracker
	if got := agEmpty.TurnChangedApplicationPaths(); got != nil {
		t.Errorf("empty window = %v, want nil", got)
	}

	// Docs-and-bookkeeping only: filtered to nil.
	agDocs := NewTestAgent()
	docsTracker := NewChangeTracker(nil, "app-paths-docs")
	docsTracker.MarkTurnStart()
	trackWrite(t, docsTracker, "/ws/README.md", "old", "new", true)
	trackEdit(t, docsTracker, "/ws/.sprout/starter.json", "old", "new")
	agDocs.changeTracker = docsTracker
	if got := agDocs.TurnChangedApplicationPaths(); got != nil {
		t.Errorf("docs-only window = %v, want nil", got)
	}
}
