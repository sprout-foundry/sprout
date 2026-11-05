package mcp

import (
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateCredentials points the credential backend at a temp dir for the
// duration of the test, so entries written here cannot leak into another
// test (or be inherited from one).
func isolateCredentials(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_CONFIG", filepath.Join(home, ".sprout"))
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)
}

// An inline bearer token in a header must not survive in the config: it is
// stored in the credential backend and replaced with a placeholder, the same
// treatment env secrets get.
func TestMigrateHeaderSecretsFromServer_Authorization(t *testing.T) {
	isolateCredentials(t)

	config := &MCPServerConfig{
		Name: "stripe",
		Type: "http",
		URL:  "https://mcp.stripe.com",
		Headers: map[string]string{
			"Authorization": "Bearer sk_test_123",
			"X-Trace":       "not-a-secret",
		},
	}

	migrated, err := MigrateHeaderSecretsFromServer("stripe", config)
	require.NoError(t, err)
	assert.Equal(t, 1, migrated)

	assert.True(t, IsSecretRef(config.Headers["Authorization"]),
		"the bearer token must be replaced with a credential placeholder")
	assert.Equal(t, "not-a-secret", config.Headers["X-Trace"],
		"a non-secret header must pass through untouched")

	stored, _, err := credentials.GetFromActiveBackend(HeaderCredentialKey("stripe", "Authorization"))
	require.NoError(t, err)
	assert.Equal(t, "Bearer sk_test_123", stored)
}

// A header named like a token is migrated even without an Authorization name.
func TestMigrateHeaderSecretsFromServer_TokenNamedHeader(t *testing.T) {
	isolateCredentials(t)

	config := &MCPServerConfig{
		Name:    "svc",
		Type:    "http",
		Headers: map[string]string{"X-Api-Key": "abc123"},
	}

	migrated, err := MigrateHeaderSecretsFromServer("svc", config)
	require.NoError(t, err)
	assert.Equal(t, 1, migrated)
	assert.True(t, IsSecretRef(config.Headers["X-Api-Key"]))
}

// A header whose name is not credential-shaped stays plaintext, even when it
// contains a word an env-var heuristic would match.
func TestMigrateHeaderSecretsFromServer_NonSecretHeaderUntouched(t *testing.T) {
	isolateCredentials(t)

	config := &MCPServerConfig{
		Name:    "svc",
		Type:    "http",
		Headers: map[string]string{"X-Trace-Id": "abc"},
	}

	migrated, err := MigrateHeaderSecretsFromServer("svc", config)
	require.NoError(t, err)
	assert.Equal(t, 0, migrated)
	assert.Equal(t, "abc", config.Headers["X-Trace-Id"])
}

// An already-migrated placeholder is left alone, so a re-save is a no-op.
func TestMigrateHeaderSecretsFromServer_Idempotent(t *testing.T) {
	isolateCredentials(t)

	config := &MCPServerConfig{
		Name:    "svc",
		Type:    "http",
		Headers: map[string]string{"Authorization": SecretRef("svc", "Authorization")},
	}

	migrated, err := MigrateHeaderSecretsFromServer("svc", config)
	require.NoError(t, err)
	assert.Equal(t, 0, migrated)
	assert.True(t, IsSecretRef(config.Headers["Authorization"]))
}

// A header edit must not silently rotate a credential the user set elsewhere:
// the stored value wins, and the config still carries a placeholder.
func TestMigrateHeaderSecretsFromServer_DoesNotClobberExisting(t *testing.T) {
	isolateCredentials(t)

	require.NoError(t, credentials.SetToActiveBackend(HeaderCredentialKey("svc", "Authorization"), "Bearer original"))

	config := &MCPServerConfig{
		Name:    "svc",
		Type:    "http",
		Headers: map[string]string{"Authorization": "Bearer different"},
	}

	migrated, err := MigrateHeaderSecretsFromServer("svc", config)
	require.NoError(t, err)
	assert.Equal(t, 0, migrated, "an existing credential must not be overwritten")

	stored, _, err := credentials.GetFromActiveBackend(HeaderCredentialKey("svc", "Authorization"))
	require.NoError(t, err)
	assert.Equal(t, "Bearer original", stored)
	assert.True(t, IsSecretRef(config.Headers["Authorization"]),
		"the config must still reference the credential it kept")
}

// A header credential and an env credential of the same name must not share a
// store entry: they are different secrets that happen to share a vocabulary.
func TestMigrateHeaderSecretsFromServer_NoCollisionWithEnvCredential(t *testing.T) {
	isolateCredentials(t)

	// An env credential named "Authorization" already exists.
	require.NoError(t, credentials.SetToActiveBackend(CredentialKey("svc", "Authorization"), "Bearer env-secret"))

	config := &MCPServerConfig{
		Name:    "svc",
		Type:    "http",
		Env:     map[string]string{"Authorization": SecretRef("svc", "Authorization")},
		Headers: map[string]string{"Authorization": "Bearer header-secret"},
	}

	migrated, err := MigrateHeaderSecretsFromServer("svc", config)
	require.NoError(t, err)
	assert.Equal(t, 1, migrated, "the header is a distinct credential, not a duplicate of the env one")

	envValue, _, err := credentials.GetFromActiveBackend(CredentialKey("svc", "Authorization"))
	require.NoError(t, err)
	assert.Equal(t, "Bearer env-secret", envValue, "the env credential must be untouched")

	headerValue, _, err := credentials.GetFromActiveBackend(HeaderCredentialKey("svc", "Authorization"))
	require.NoError(t, err)
	assert.Equal(t, "Bearer header-secret", headerValue)

	// Both resolve independently on the wire.
	headers, err := buildRequestHeaders("svc", config)
	require.NoError(t, err)
	assert.Equal(t, "Bearer header-secret", headers["Authorization"],
		"the explicit header must win over the env-derived one")
}

// A migrated header still resolves on the wire: buildRequestHeaders reads the
// placeholder back out of the credential store.
func TestMigrateHeaderSecretsFromServer_ResolvesOnRequest(t *testing.T) {
	isolateCredentials(t)

	config := &MCPServerConfig{
		Name:    "stripe",
		Type:    "http",
		Headers: map[string]string{"Authorization": "Bearer sk_live_xyz"},
	}
	_, err := MigrateHeaderSecretsFromServer("stripe", config)
	require.NoError(t, err)

	headers, err := buildRequestHeaders("stripe", config)
	require.NoError(t, err)
	assert.Equal(t, "Bearer sk_live_xyz", headers["Authorization"])
}
