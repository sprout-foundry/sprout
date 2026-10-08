//go:build !js

package cmd

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withIsolatedConfig points this process's config root at a temp dir, so
// localConfigDirForComparison resolves to a known, isolated value the test
// can compare against the (stub) daemon's identity. Returns the resolved dir.
func withIsolatedConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG_DIR", "")
	resolved, err := envutil.ConfigDir()
	require.NoError(t, err)
	return resolved
}

// TestTryDaemonOneShot_VersionMismatchRunsInProcess pins the core contract:
// a daemon whose binary version differs from the invoking binary's must
// never silently receive the turn. Routing falls back to in-process and the
// daemon's Query is never called.
func TestTryDaemonOneShot_VersionMismatchRunsInProcess(t *testing.T) {
	withIsolatedConfig(t)

	localDir := localConfigDirForComparison()
	stub := &stubAgentForCmd{identity: &daemon.DaemonIdentity{
		Version:   "v9.9.9",
		Commit:    "deadbee",
		ConfigDir: localDir, // config matches — only the version differs
	}}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.False(t, handled, "a version-mismatched daemon must not serve the turn")
	assert.Zero(t, atomic.LoadInt64(&stub.queries), "the daemon's Query must never be called on a version mismatch")
}

// TestTryDaemonOneShot_ConfigMismatchRunsInProcess is the --isolated-config
// regression: the daemon's config root differs from this run's (the daemon
// still uses the user's main config while the CLI points at ./.sprout).
// Routing must fall back to in-process, and the daemon's Query must not run.
func TestTryDaemonOneShot_ConfigMismatchRunsInProcess(t *testing.T) {
	withIsolatedConfig(t)

	otherDir := filepath.Join(t.TempDir(), "main-config")
	stub := &stubAgentForCmd{identity: &daemon.DaemonIdentity{
		Version:   buildinfoVersionForTest(),
		Commit:    "",
		ConfigDir: otherDir, // different config root — the isolated-config bug
	}}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.False(t, handled, "a config-mismatched daemon must not serve the turn")
	assert.Zero(t, atomic.LoadInt64(&stub.queries), "the daemon's Query must never be called on a config mismatch")
}

// TestTryDaemonOneShot_MatchingIdentityRoutes pins the positive path: a
// daemon reporting the same version and the same config root as this run is
// a compatible place to run the turn, so it serves it.
func TestTryDaemonOneShot_MatchingIdentityRoutes(t *testing.T) {
	withIsolatedConfig(t)

	stub := &stubAgentForCmd{identity: &daemon.DaemonIdentity{
		Version:   buildinfoVersionForTest(),
		Commit:    "",
		ConfigDir: localConfigDirForComparison(),
	}}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.True(t, handled, "a version/config-matched daemon must serve the turn")
	assert.Equal(t, int64(1), atomic.LoadInt64(&stub.queries), "the daemon served the query")
}

// TestTryDaemonOneShot_ConfigRelativeAndAbsoluteCompareEqual verifies the
// config comparison normalizes path spelling: the daemon may resolve its
// config root relative while this process resolves it absolute. The two must
// still be recognized as the same directory.
func TestTryDaemonOneShot_ConfigRelativeAndAbsoluteCompareEqual(t *testing.T) {
	localDir := withIsolatedConfig(t)

	// A relative path to the same directory must compare equal to the
	// absolute one this process resolved.
	rel, err := filepath.Rel(mustGetwd(t), localDir)
	require.NoError(t, err)

	stub := &stubAgentForCmd{identity: &daemon.DaemonIdentity{
		Version:   buildinfoVersionForTest(),
		ConfigDir: rel,
	}}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.True(t, handled, "a relative path to the same config dir must be recognized as matching")
	assert.Equal(t, int64(1), atomic.LoadInt64(&stub.queries))
}

// TestTryDaemonOneShot_EmptyConfigDirNeverMatches verifies an unknown
// (empty) config root on either side is treated as a mismatch, never as
// "equal": the daemon must not run the turn against a config it can't name.
func TestTryDaemonOneShot_EmptyConfigDirNeverMatches(t *testing.T) {
	withIsolatedConfig(t)

	stub := &stubAgentForCmd{identity: &daemon.DaemonIdentity{
		Version:   buildinfoVersionForTest(),
		ConfigDir: "", // daemon can't name its config
	}}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.False(t, handled, "an empty daemon config root must not be assumed equal to this run's")
	assert.Zero(t, atomic.LoadInt64(&stub.queries))
}

// TestTryDaemonOneShot_NoDaemonFlagSkipsRouting pins --no-daemon: the flag
// trips the routing gate so the turn runs in-process even with a healthy,
// compatible daemon.
func TestTryDaemonOneShot_NoDaemonFlagSkipsRouting(t *testing.T) {
	saveAgentFlagVars(t)
	withIsolatedConfig(t)
	agentNoDaemon = true

	stub := &stubAgentForCmd{}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_DAEMON_AGENT", "1")

	assert.True(t, agentSkipDaemonRouting(), "--no-daemon must trip the routing gate")

	handled, err := tryDaemonOneShot(context.Background(), "hello", false)
	require.NoError(t, err)
	assert.False(t, handled, "--no-daemon must force in-process execution")
	assert.Zero(t, atomic.LoadInt64(&stub.queries), "no query may reach the daemon")
}

// TestDaemonIdentityMismatch_ComparisonRules pins the comparison rules
// directly, including the "dev" vs "dev" equality that keeps the daemon path
// usable on source builds.
func TestDaemonIdentityMismatch_ComparisonRules(t *testing.T) {
	local := t.TempDir()
	require.NoError(t, os.MkdirAll(local, 0o700))
	localAbs := normalizeConfigDir(local)

	cases := []struct {
		name      string
		ident     daemon.DaemonIdentity
		localDir  string
		wantMatch bool // true = identities are compatible
	}{
		{
			name:      "same dev version, same config",
			ident:     daemon.DaemonIdentity{Version: "dev", ConfigDir: local},
			localDir:  local,
			wantMatch: true,
		},
		{
			name:      "dev vs release is a mismatch",
			ident:     daemon.DaemonIdentity{Version: "v1.2.3", ConfigDir: local},
			localDir:  local,
			wantMatch: false,
		},
		{
			name:      "empty daemon version is a mismatch",
			ident:     daemon.DaemonIdentity{Version: "", ConfigDir: local},
			localDir:  local,
			wantMatch: false,
		},
		{
			name:      "same version, different config is a mismatch",
			ident:     daemon.DaemonIdentity{Version: buildinfoVersionForTest(), ConfigDir: filepath.Join(local, "other")},
			localDir:  local,
			wantMatch: false,
		},
		{
			name:      "same version, empty daemon config is a mismatch",
			ident:     daemon.DaemonIdentity{Version: buildinfoVersionForTest(), ConfigDir: ""},
			localDir:  local,
			wantMatch: false,
		},
		{
			name:      "same version, empty local config is a mismatch",
			ident:     daemon.DaemonIdentity{Version: buildinfoVersionForTest(), ConfigDir: local},
			localDir:  "",
			wantMatch: false,
		},
		{
			name:      "relative daemon path matches absolute local path",
			ident:     daemon.DaemonIdentity{Version: buildinfoVersionForTest(), ConfigDir: local},
			localDir:  localAbs,
			wantMatch: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mismatch, msg := daemonIdentityMismatch(&tc.ident, tc.localDir)
			if tc.wantMatch {
				assert.False(t, mismatch, "expected compatible identities, got mismatch: %s", msg)
				return
			}
			assert.True(t, mismatch, "expected a mismatch for %+v vs %q", tc.ident, tc.localDir)
			assert.NotEmpty(t, msg, "a mismatch must explain itself")
		})
	}
}

// TestDaemonIdentityMismatch_NamesBothSides pins the honesty requirement: a
// mismatch message names both the daemon's and this run's version/config, so
// the user can see exactly what differed.
func TestDaemonIdentityMismatch_NamesBothSides(t *testing.T) {
	local := t.TempDir()

	t.Run("version mismatch names both versions", func(t *testing.T) {
		mismatch, msg := daemonIdentityMismatch(
			&daemon.DaemonIdentity{Version: "v9.9.9", Commit: "deadbee", ConfigDir: local},
			local,
		)
		require.True(t, mismatch)
		assert.Contains(t, msg, "v9.9.9", "message must name the daemon's version")
		assert.Contains(t, msg, buildinfoVersionForTest(), "message must name this binary's version")
	})

	t.Run("config mismatch names both configs", func(t *testing.T) {
		daemonDir := filepath.Join(local, "daemon-config")
		mismatch, msg := daemonIdentityMismatch(
			&daemon.DaemonIdentity{Version: buildinfoVersionForTest(), ConfigDir: daemonDir},
			local,
		)
		require.True(t, mismatch)
		assert.Contains(t, msg, daemonDir, "message must name the daemon's config")
		assert.Contains(t, msg, local, "message must name this run's config")
	})
}

// TestDaemonIdentityMismatch_CommitRule pins the build-commit rule that
// catches two different "dev" binaries: when both sides know their commit
// and the commits differ, that is a mismatch even though the version strings
// are equal. An empty commit on either side is unknown and not compared.
func TestDaemonIdentityMismatch_CommitRule(t *testing.T) {
	local := t.TempDir()

	origCommit := buildinfo.Commit
	t.Cleanup(func() { buildinfo.Commit = origCommit })

	// Both sides know a commit — a difference must be caught.
	buildinfo.Commit = "aaaaaaa"
	mismatch, msg := daemonIdentityMismatch(
		&daemon.DaemonIdentity{Version: buildinfo.Version, Commit: "bbbbbbb", ConfigDir: local},
		local,
	)
	require.True(t, mismatch, "differing known commits must be a mismatch even with equal versions")
	assert.Contains(t, msg, "aaaaaaa")
	assert.Contains(t, msg, "bbbbbbb")

	// Same known commit — compatible.
	mismatch, _ = daemonIdentityMismatch(
		&daemon.DaemonIdentity{Version: buildinfo.Version, Commit: "aaaaaaa", ConfigDir: local},
		local,
	)
	assert.False(t, mismatch, "identical known commits must be compatible")

	// Local commit unknown (source run with no injected commit) — not
	// compared, so a daemon from the same source still routes.
	buildinfo.Commit = ""
	mismatch, _ = daemonIdentityMismatch(
		&daemon.DaemonIdentity{Version: buildinfo.Version, Commit: "bbbbbbb", ConfigDir: local},
		local,
	)
	assert.False(t, mismatch, "an unknown local commit must not be treated as a mismatch")

	// Daemon commit unknown — likewise not compared.
	buildinfo.Commit = "aaaaaaa"
	mismatch, _ = daemonIdentityMismatch(
		&daemon.DaemonIdentity{Version: buildinfo.Version, Commit: "", ConfigDir: local},
		local,
	)
	assert.False(t, mismatch, "an unknown daemon commit must not be treated as a mismatch")
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	return wd
}

// buildinfoVersionForTest returns the invoking binary's buildinfo version —
// what a compatible daemon must report. Kept in one place so the tests don't
// each reach for the package global.
func buildinfoVersionForTest() string {
	return buildinfo.Version
}

// TestFormatBinaryIdentity pins the rendering: version alone, and
// version+commit; an empty version is named "unknown" rather than blank.
func TestFormatBinaryIdentity(t *testing.T) {
	assert.Equal(t, "dev", formatBinaryIdentity("dev", ""))
	assert.Equal(t, "v1.2.3 (a1b2c3d)", formatBinaryIdentity("v1.2.3", "a1b2c3d"))
	assert.Equal(t, "unknown", formatBinaryIdentity("", ""))
}

// TestDisplayConfigDir pins the empty-config rendering: an unresolved config
// root reads "(unresolved)", never blank.
func TestDisplayConfigDir(t *testing.T) {
	assert.Equal(t, "/some/config", displayConfigDir("/some/config"))
	assert.Equal(t, "(unresolved)", displayConfigDir(""))
}

// TestNormalizeConfigDir pins the comparison normalization: an empty root
// stays empty (never the cwd), and a relative path resolves to absolute.
func TestNormalizeConfigDir(t *testing.T) {
	assert.Equal(t, "", normalizeConfigDir(""))
	wd := mustGetwd(t)
	assert.Equal(t, normalizeConfigDir(wd), normalizeConfigDir("."))
}
