package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPServerConfig_TrustUntrustedDefaultsTrueWhenAbsent pins the SP-169
// default: a server parsed from JSON that predates the framing setting must
// frame its output. The struct field's zero value is false, so a naive
// implementation would silently read "do not frame" for every existing server —
// the opposite of the intended default.
func TestMCPServerConfig_TrustUntrustedDefaultsTrueWhenAbsent(t *testing.T) {
	raw := []byte(`{
		"name": "github-mcp",
		"type": "stdio",
		"command": "npx",
		"auto_start": true,
		"max_restarts": 3
	}`)

	var srv MCPServerConfig
	require.NoError(t, json.Unmarshal(raw, &srv))

	assert.True(t, srv.TrustUntrustedContent,
		"a server with no recorded trust_untrusted_content must default to framing on")
	assert.True(t, srv.TrustUntrustedContentOrDefault())
}

// TestMCPServerConfig_TrustUntrustedExplicitFalseSurvives covers the other
// direction: an explicit opt-out is a decision and must not be overwritten by
// the default on the next load.
func TestMCPServerConfig_TrustUntrustedExplicitFalseSurvives(t *testing.T) {
	raw := []byte(`{"name": "internal-mcp", "trust_untrusted_content": false}`)

	var srv MCPServerConfig
	require.NoError(t, json.Unmarshal(raw, &srv))

	assert.False(t, srv.TrustUntrustedContent)
	assert.False(t, srv.TrustUntrustedContentOrDefault(),
		"an explicit false must survive the default normalization")

	// And it must round-trip through a save, or the opt-out would be lost.
	out, err := json.Marshal(srv)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"trust_untrusted_content":false`)

	var back MCPServerConfig
	require.NoError(t, json.Unmarshal(out, &back))
	assert.False(t, back.TrustUntrustedContentOrDefault())
}

// TestMCPServerConfig_TrustUntrustedExplicitTrueRoundTrips covers the recorded
// true: the value is written out, so a later read does not have to guess.
func TestMCPServerConfig_TrustUntrustedExplicitTrueRoundTrips(t *testing.T) {
	srv := MCPServerConfig{Name: "github-mcp"}
	srv.SetTrustUntrustedContent(true)

	out, err := json.Marshal(srv)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"trust_untrusted_content":true`)

	var back MCPServerConfig
	require.NoError(t, json.Unmarshal(out, &back))
	assert.True(t, back.TrustUntrustedContentOrDefault())
}

// TestTrustUntrustedContentOrDefault_ZeroValueStructFrames is the in-code
// construction case: a struct literal that never set the field (an
// auto-discovered server, a test fixture) still gets framing, because the
// setting was never recorded as a choice.
func TestTrustUntrustedContentOrDefault_ZeroValueStructFrames(t *testing.T) {
	srv := MCPServerConfig{Name: "auto-discovered"}
	assert.False(t, srv.TrustUntrustedContent, "the raw field is the bool zero value")
	assert.True(t, srv.TrustUntrustedContentOrDefault(),
		"an unrecorded setting defaults to framing on")
}

// TestLoadMCPConfig_NormalizesTrustUntrusted drives the default through the real
// load path, not just the unmarshaler: a config file on disk without the field
// must come back with framing on for every server.
func TestLoadMCPConfig_NormalizesTrustUntrusted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", dir)

	path := filepath.Join(dir, mcpConfigFileName)
	require.NoError(t, os.WriteFile(path, []byte(`{
		"enabled": true,
		"servers": {
			"legacy": {"name": "legacy", "command": "npx", "args": ["-y", "server"]},
			"opted-out": {"name": "opted-out", "command": "npx", "trust_untrusted_content": false}
		}
	}`), 0o600))

	cfg, err := LoadMCPConfig()
	require.NoError(t, err)

	legacy, ok := cfg.Servers["legacy"]
	require.True(t, ok, "legacy server missing from loaded config")
	assert.True(t, legacy.TrustUntrustedContentOrDefault(),
		"a server with no recorded setting must frame after a real load")

	optedOut, ok := cfg.Servers["opted-out"]
	require.True(t, ok, "opted-out server missing from loaded config")
	assert.False(t, optedOut.TrustUntrustedContentOrDefault(),
		"an explicit opt-out must survive a real load")
}
