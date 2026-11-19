package credentials

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateToDir points the ambient credential scope at dir (creating a fresh
// key) for the test's duration, so stores written under one scope cannot
// leak into another test.
func isolateToDir(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend()
	t.Cleanup(ResetStorageBackend)
}

// A store file that does not exist is not an error: an empty Store comes
// back, matching Load()'s contract.
func TestLoadStoreFromDir_MissingFile(t *testing.T) {
	dir := t.TempDir()
	store, err := LoadStoreFromDir(dir)
	require.NoError(t, err)
	assert.Empty(t, store)
}

// A missing config directory argument is an error, not a silent empty store.
func TestLoadStoreFromDir_EmptyDir(t *testing.T) {
	_, err := LoadStoreFromDir("")
	assert.Error(t, err)
}

// Plaintext (legacy) store files are read back without a key.
func TestLoadStoreFromDir_PlaintextJSON(t *testing.T) {
	dir := t.TempDir()
	credDir := filepath.Join(dir, "credentials")
	require.NoError(t, os.MkdirAll(credDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(credDir, "api_keys.json"), []byte(`{"openai":"sk-plain"}`), 0o600))

	store, err := LoadStoreFromDir(dir)
	require.NoError(t, err)
	assert.Equal(t, "sk-plain", store["openai"])
}

// The whole point of the dir-scoped read: a store written under one scope
// (its own key.age) is decryptable from a process whose ambient scope points
// elsewhere — the daemon case, where `sprout login` ran with SPROUT_CONFIG
// set but the daemon resolves the global store.
func TestLoadStoreFromDir_EncryptedWithDirKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wsDir := filepath.Join(home, "ws", ".sprout")
	globalDir := filepath.Join(home, "global")

	// Write the store under the workspace scope (creates ws key.age + store).
	isolateToDir(t, wsDir)
	require.NoError(t, SetToActiveBackend("mcp/foundry/header/Authorization", "Bearer ws-token"))

	// Switch the process's ambient scope to an empty global dir.
	isolateToDir(t, globalDir)
	ambient, _, err := GetFromActiveBackend("mcp/foundry/header/Authorization")
	require.NoError(t, err)
	assert.Empty(t, ambient, "the ambient scope must not see the workspace store")

	store, err := LoadStoreFromDir(wsDir)
	require.NoError(t, err)
	assert.Equal(t, "Bearer ws-token", store["mcp/foundry/header/Authorization"],
		"the store must decrypt with the directory's own key")
}

// A workspace store written under one HOME is unreadable (not empty, not
// leaked) when the key age is absent from that directory: the error is
// surfaced so a caller can distinguish "no value" from "unreadable store".
func TestLoadStoreFromDir_WrongKeyErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wsDir := filepath.Join(home, "ws", ".sprout")
	otherDir := filepath.Join(home, "other")

	isolateToDir(t, wsDir)
	require.NoError(t, SetToActiveBackend("provider/key", "secret"))

	// Copy just the store file into another dir, without its key.age.
	wsCred := filepath.Join(wsDir, "credentials")
	otherCred := filepath.Join(otherDir, "credentials")
	require.NoError(t, os.MkdirAll(otherCred, 0o700))
	data, err := os.ReadFile(filepath.Join(wsCred, "api_keys.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(otherCred, "api_keys.json"), data, 0o600))

	_, err = LoadStoreFromDir(otherDir)
	assert.Error(t, err, "decrypting with a missing key must fail, not return a partial store")
}
