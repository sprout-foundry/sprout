package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clearWorkspaceCredDir resets the process-level fallback and restores it on
// cleanup so one test's registration cannot leak into another.
func clearWorkspaceCredDir(t *testing.T) {
	t.Helper()
	prev := GetWorkspaceCredentialDir()
	SetWorkspaceCredentialDir("")
	t.Cleanup(func() { SetWorkspaceCredentialDir(prev) })
}

// Write a credential into an explicit store dir so the fallback can read it
// without going through the ambient (SPROUT_CONFIG) scope.
func writeStoreDir(t *testing.T, dir, key, value string) {
	t.Helper()
	credDir := filepath.Join(dir, "credentials")
	require.NoError(t, os.MkdirAll(credDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(credDir, "api_keys.json"),
		[]byte(`{"`+key+`":"`+value+`"}`), 0o600))
}

const foundryAuthKey = "mcp/sprout-foundry/header/Authorization"

// The daemon case: the token lives in the workspace store, but the process's
// ambient scope (SPROUT_CONFIG) points at an empty global store. The fallback
// must surface the workspace value so the Authorization header is populated
// instead of silently dropped.
func TestGetMCPHeaderCredential_WorkspaceFallback(t *testing.T) {
	isolateCredentials(t)
	clearWorkspaceCredDir(t)

	// Ambient scope is empty; the workspace store holds the token.
	ws := t.TempDir()
	writeStoreDir(t, ws, foundryAuthKey, "Bearer ws-token")
	SetWorkspaceCredentialDir(ws)

	ambient, _, err := credentials.GetFromActiveBackend(foundryAuthKey)
	require.NoError(t, err)
	assert.Empty(t, ambient, "ambient scope must not hold the key")

	value, source := getMCPHeaderCredential(foundryAuthKey)
	assert.Equal(t, "Bearer ws-token", value)
	assert.Equal(t, "workspace", source)
}

// When both scopes hold the key, the process's own (ambient) store wins.
func TestGetMCPHeaderCredential_AmbientWins(t *testing.T) {
	isolateCredentials(t)
	clearWorkspaceCredDir(t)

	ambientDir := os.Getenv("SPROUT_CONFIG")
	require.NotEmpty(t, ambientDir)
	require.NoError(t, credentials.SetToActiveBackend(foundryAuthKey, "Bearer ambient"))

	ws := t.TempDir()
	writeStoreDir(t, ws, foundryAuthKey, "Bearer workspace")
	SetWorkspaceCredentialDir(ws)

	value, source := getMCPHeaderCredential(foundryAuthKey)
	assert.Equal(t, "Bearer ambient", value, "the process's own store must win")
	assert.NotEqual(t, "workspace", source)
}

// No fallback registered and no ambient value: both empty, so the header is
// dropped by the caller.
func TestGetMCPHeaderCredential_NotSetAnywhere(t *testing.T) {
	isolateCredentials(t)
	clearWorkspaceCredDir(t)

	value, source := getMCPHeaderCredential(foundryAuthKey)
	assert.Empty(t, value)
	assert.Empty(t, source)
}

// End-to-end on the wire: a header built from a credential placeholder
// resolves through the workspace fallback, so buildRequestHeaders emits a
// real Authorization header rather than dropping it.
func TestBuildRequestHeaders_ResolvesWorkspaceFallback(t *testing.T) {
	isolateCredentials(t)
	clearWorkspaceCredDir(t)

	ws := t.TempDir()
	writeStoreDir(t, ws, foundryAuthKey, "Bearer foundry-token")
	SetWorkspaceCredentialDir(ws)

	config := &MCPServerConfig{
		Name: "sprout-foundry",
		Type: "http",
		URL:  "https://app.sproutfoundry.com/mcp",
		Headers: map[string]string{
			"Authorization": "{{credential:" + foundryAuthKey + "}}",
		},
	}

	headers, err := buildRequestHeaders("sprout-foundry", config)
	require.NoError(t, err)
	assert.Equal(t, "Bearer foundry-token", headers["Authorization"],
		"the platform Authorization header must be populated from the workspace store")
}

// The env-var credential path (stdio servers) gets the same fallback: a
// {{credential:...}} placeholder in the Credentials map resolves from the
// workspace store when the ambient scope is empty.
func TestBuildFullEnvForServer_WorkspaceFallback(t *testing.T) {
	isolateCredentials(t)
	clearWorkspaceCredDir(t)

	key := CredentialKey("gh", "GITHUB_TOKEN")
	ws := t.TempDir()
	writeStoreDir(t, ws, key, "ghp_ws")
	SetWorkspaceCredentialDir(ws)

	config := &MCPServerConfig{
		Name:        "gh",
		Type:        "stdio",
		Credentials: map[string]string{"GITHUB_TOKEN": "{{credential:" + key + "}}"},
	}

	env, err := BuildFullEnvForServer("gh", config)
	require.NoError(t, err)
	assert.Equal(t, "ghp_ws", env["GITHUB_TOKEN"],
		"the env credential must resolve from the workspace store")
}
