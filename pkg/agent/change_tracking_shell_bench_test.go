package agent

import (
	"testing"
)

// BenchmarkCaptureShellSnapshot_SproutRepo measures a COLD full snapshot
// — the prime cost paid once per agent session.
//
// Run with:
//
//	go test -tags grammar_blobs_external -run X -bench BenchmarkCaptureShellSnapshot_SproutRepo -benchmem ./pkg/agent/
func BenchmarkCaptureShellSnapshot_SproutRepo(b *testing.B) {
	tracker := &ChangeTracker{enabled: true}
	root := "../.."
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snap := tracker.captureShellSnapshot(root)
		if len(snap) == 0 {
			b.Fatal("empty snapshot — root not resolving correctly")
		}
	}
}

// BenchmarkTrackShellTurn_WarmNoChanges measures the per-shell_command
// cost AFTER the cache has been primed and the workspace hasn't
// changed. This is the realistic steady-state cost — most agent
// shell commands don't mutate files (ls, grep, build, test, …). The
// fast path lets us stat-walk without re-reading content for unchanged
// files, so this should be ~5–20 ms regardless of repo size.
//
// Compare against BenchmarkCaptureShellSnapshot_SproutRepo (~30 ms)
// for the cold-walk baseline to see the speedup.
func BenchmarkTrackShellTurn_WarmNoChanges(b *testing.B) {
	tracker := &ChangeTracker{enabled: true}
	root := "../.."
	tracker.PrimeShellTracking(root) // pay the cold cost once
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.TrackShellTurn(root, "shell_command")
	}
}

// BenchmarkShellLooksReadOnly measures the cost of the read-only
// classifier. This runs on every shell_command before deciding whether
// to snapshot — needs to be cheap (microseconds) so the short-circuit
// itself isn't a bottleneck.
func BenchmarkShellLooksReadOnly(b *testing.B) {
	cmds := []string{
		"ls -la",
		"grep -r foo .",
		"git status",
		"cat README.md",
		"sed -i 's/foo/bar/' file.txt", // unsafe path
		"go build ./...",                // unsafe path
		"find . -name '*.go' | xargs wc -l", // pipe path
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = shellLooksReadOnly(cmds[i%len(cmds)])
	}
}
