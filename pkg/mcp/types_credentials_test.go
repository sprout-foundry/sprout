package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// GetCredentialNames Tests
// ---------------------------------------------------------------------------

func TestGetCredentialNames_WithCredentials(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Credentials: map[string]string{
			"API_KEY":     "secret-key",
			"ACCESS_TOKEN": "token-123",
			"PASSWORD":     "pass-456",
		},
	}

	names := config.GetCredentialNames()

	assert.Len(t, names, 3)
	assert.Contains(t, names, "API_KEY")
	assert.Contains(t, names, "ACCESS_TOKEN")
	assert.Contains(t, names, "PASSWORD")
}

func TestGetCredentialNames_WithSingleCredential(t *testing.T) {
	config := MCPServerConfig{
		Name: "single-server",
		Credentials: map[string]string{
			"TOKEN": "secret",
		},
	}

	names := config.GetCredentialNames()

	assert.Len(t, names, 1)
	assert.Equal(t, "TOKEN", names[0])
}

func TestGetCredentialNames_NilCredentials(t *testing.T) {
	config := MCPServerConfig{
		Name:        "nil-server",
		Credentials: nil,
	}

	names := config.GetCredentialNames()

	assert.Nil(t, names, "should return nil when credentials map is nil")
}

func TestGetCredentialNames_EmptyCredentials(t *testing.T) {
	config := MCPServerConfig{
		Name:        "empty-server",
		Credentials: map[string]string{},
	}

	names := config.GetCredentialNames()

	assert.Nil(t, names, "should return nil when credentials map is empty")
}

func TestGetCredentialNames_OrderNotGuaranteed(t *testing.T) {
	config := MCPServerConfig{
		Name: "order-server",
		Credentials: map[string]string{
			"KEY_A": "value-a",
			"KEY_B": "value-b",
			"KEY_C": "value-c",
		},
	}

	names := config.GetCredentialNames()

	// Check we got all three keys, regardless of order
	assert.Len(t, names, 3)
	keySet := make(map[string]bool)
	for _, name := range names {
		keySet[name] = true
	}
	assert.True(t, keySet["KEY_A"])
	assert.True(t, keySet["KEY_B"])
	assert.True(t, keySet["KEY_C"])
}

// ---------------------------------------------------------------------------
// HasCredentials Tests
// ---------------------------------------------------------------------------

func TestHasCredentials_WithCredentials(t *testing.T) {
	config := MCPServerConfig{
		Name: "has-creds-server",
		Credentials: map[string]string{
			"API_KEY": "secret",
		},
	}

	assert.True(t, config.HasCredentials(), "should return true when credentials map is populated")
}

func TestHasCredentials_WithMultipleCredentials(t *testing.T) {
	config := MCPServerConfig{
		Name: "multiple-creds-server",
		Credentials: map[string]string{
			"API_KEY":      "secret-1",
			"ACCESS_TOKEN": "secret-2",
			"PASSWORD":     "secret-3",
		},
	}

	assert.True(t, config.HasCredentials(), "should return true with multiple credentials")
}

func TestHasCredentials_NilCredentials(t *testing.T) {
	config := MCPServerConfig{
		Name:        "nil-creds-server",
		Credentials: nil,
	}

	assert.False(t, config.HasCredentials(), "should return false when credentials map is nil")
}

func TestHasCredentials_EmptyCredentials(t *testing.T) {
	config := MCPServerConfig{
		Name:        "empty-creds-server",
		Credentials: map[string]string{},
	}

	assert.False(t, config.HasCredentials(), "should return false when credentials map is empty")
}

func TestHasCredentials_WithEnvButNoCredentials(t *testing.T) {
	config := MCPServerConfig{
		Name: "env-only-server",
		Env: map[string]string{
			"PATH": "/usr/bin",
			"HOME": "/home/user",
		},
		Credentials: nil,
	}

	assert.False(t, config.HasCredentials(), "should return false when only Env vars are set, not Credentials")
}

func TestHasCredentials_BothEnvAndCredentials(t *testing.T) {
	config := MCPServerConfig{
		Name: "both-server",
		Env: map[string]string{
			"PATH": "/usr/bin",
		},
		Credentials: map[string]string{
			"API_KEY": "secret",
		},
	}

	assert.True(t, config.HasCredentials(), "should return true when Credentials map has entries (Env presence doesn't matter)")
}

// ---------------------------------------------------------------------------
// Combined Behavior Tests
// ---------------------------------------------------------------------------

func TestCredentials_NamesAndHasConsistent(t *testing.T) {
	// When HasCredentials returns true, GetCredentialNames should return non-nil
	config := MCPServerConfig{
		Name:        "consistent-server",
		Credentials: map[string]string{"TOKEN": "secret"},
	}

	if config.HasCredentials() {
		names := config.GetCredentialNames()
		assert.NotNil(t, names, "HasCredentials true implies GetCredentialNames non-nil")
		assert.NotEmpty(t, names, "HasCredentials true implies GetCredentialNames has entries")
	}
}

func TestCredentials_NilAndHasConsistent(t *testing.T) {
	// When HasCredentials returns false, GetCredentialNames should return nil or empty
	config := MCPServerConfig{
		Name:        "nil-consistent-server",
		Credentials: nil,
	}

	if !config.HasCredentials() {
		names := config.GetCredentialNames()
		assert.Nil(t, names, "HasCredentials false with nil map implies GetCredentialNames nil")
	}
}

func TestCredentials_EmptyAndHasConsistent(t *testing.T) {
	// When HasCredentials returns false for empty map, GetCredentialNames should return nil
	config := MCPServerConfig{
		Name:        "empty-consistent-server",
		Credentials: map[string]string{},
	}

	if !config.HasCredentials() {
		names := config.GetCredentialNames()
		assert.Nil(t, names, "HasCredentials false with empty map implies GetCredentialNames nil")
	}
}
