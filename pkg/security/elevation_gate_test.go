package security

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockPrompter implements SecretPrompter for testing.
type mockPrompter struct {
	action SecretAction
	err    error
}

func (m *mockPrompter) PromptSecretAction(_ []DetectedSecret, _ string) (SecretAction, error) {
	return m.action, m.err
}

// TestEvaluate_NoSecrets verifies that an empty secrets slice returns SecretAllow.
func TestEvaluate_NoSecrets(t *testing.T) {
	g := NewElevationGate(nil)
	action, err := g.Evaluate(nil, "test")
	assert.NoError(t, err)
	assert.Equal(t, SecretAllow, action)

	action, err = g.Evaluate([]DetectedSecret{}, "test")
	assert.NoError(t, err)
	assert.Equal(t, SecretAllow, action)
}

// TestEvaluate_NonInteractiveDefault verifies that a nil prompter defaults
// to SecretRedact when secrets are detected.
func TestEvaluate_NonInteractiveDefault(t *testing.T) {
	g := NewElevationGate(nil)
	secrets := []DetectedSecret{
		{Type: "API Key", Snippet: "sk-abc123456789012345678901234567890"},
	}
	action, err := g.Evaluate(secrets, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretRedact, action)
}

// TestEvaluate_CachedDecision verifies that evaluating the same secret twice
// uses the cached result the second time, even with a nil prompter.
func TestEvaluate_CachedDecision(t *testing.T) {
	g := NewElevationGate(nil)

	// First evaluation with nil prompter → auto-redact, caches decision.
	secret := DetectedSecret{Type: "API Key", Snippet: "sk-cachedtest9876543210abcdefghijklmnop"}
	action1, err := g.Evaluate([]DetectedSecret{secret}, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretRedact, action1)

	// Second evaluation with the same secret should hit the cache.
	action2, err := g.Evaluate([]DetectedSecret{secret}, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretRedact, action2)

	// Verify it's actually cached by checking the vault size.
	assert.Contains(t, g.vault, vaultKey(secret))
}

// TestEvaluate_MultipleSecretsSameAction verifies that multiple secrets all
// resolving to the same action return that action.
func TestEvaluate_MultipleSecretsSameAction(t *testing.T) {
	p := &mockPrompter{action: SecretAllow}
	g := NewElevationGate(p)

	secrets := []DetectedSecret{
		{Type: "API Key", Snippet: "sk-firstsecret9876543210abcdefghijklmn"},
		{Type: "API Key", Snippet: "sk-secondsecret9876543210abcdefghijklmn"},
	}
	action, err := g.Evaluate(secrets, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretAllow, action)
}

// TestEvaluate_BlockWins verifies that if even one secret is blocked,
// the result is SecretBlock regardless of other actions.
func TestEvaluate_BlockWins(t *testing.T) {
	g := NewElevationGate(nil)

	// First secret → auto-redact and cache.
	s1 := DetectedSecret{Type: "API Key", Snippet: "sk-allowsecret9876543210abcdefghijklmn"}
	action1, err := g.Evaluate([]DetectedSecret{s1}, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretRedact, action1)

	// Now override the cached action for s1 to SecretAllow.
	g.vault[vaultKey(s1)] = SecretAllow

	// Second secret is new → auto-redact.
	s2 := DetectedSecret{Type: "Password", Snippet: "passblocksecret9876543210abcdefghijklmn"}
	action2, err := g.Evaluate([]DetectedSecret{s2}, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretRedact, action2)

	// Override s2 to SecretBlock.
	g.vault[vaultKey(s2)] = SecretBlock

	// Evaluate both: s1=Allow, s2=Block → Block wins.
	action3, err := g.Evaluate([]DetectedSecret{s1, s2}, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretBlock, action3)
}

// TestEvaluate_AllowWinsWhenNoBlock verifies that when all secrets are allowed,
// the result is SecretAllow.
func TestEvaluate_AllowWinsWhenNoBlock(t *testing.T) {
	g := NewElevationGate(nil)

	s1 := DetectedSecret{Type: "API Key", Snippet: "sk-firstkey9876543210abcdefghijklmnop"}
	s2 := DetectedSecret{Type: "Token", Snippet: "toksecondkey9876543210abcdefghijklmnop"}

	// Cache both as Allow.
	g.vault[vaultKey(s1)] = SecretAllow
	g.vault[vaultKey(s2)] = SecretAllow

	action, err := g.Evaluate([]DetectedSecret{s1, s2}, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretAllow, action)
}

// TestEvaluate_MixedRedactAndAllow verifies that with some secrets redacted
// and some allowed (but none blocked), the result is SecretRedact.
func TestEvaluate_MixedRedactAndAllow(t *testing.T) {
	g := NewElevationGate(nil)

	s1 := DetectedSecret{Type: "API Key", Snippet: "sk-allowkey9999876543210abcdefghijklmnop"}
	s2 := DetectedSecret{Type: "Password", Snippet: "pwredactkey8765432109abcdefghijklmnopqrstuv"}

	// s1 = Allow, s2 new → auto-redact.
	g.vault[vaultKey(s1)] = SecretAllow

	action, err := g.Evaluate([]DetectedSecret{s1, s2}, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretRedact, action)
}

// TestSetDefault_GlobalCatchAll verifies that a global catch-all (empty
// envVarName) defaults all uncached secrets to the set action.
func TestSetDefault_GlobalCatchAll(t *testing.T) {
	g := NewElevationGate(nil)
	g.SetDefault(SecretAllow, "") // global catch-all

	secrets := []DetectedSecret{
		{Type: "API Key", Snippet: "sk-catchalltest9876543210abcdefghijklmnopq"},
	}
	action, err := g.Evaluate(secrets, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretAllow, action)
}

// TestSetDefault_TypeWildcard verifies that SetDefault with a type name
// acts as a wildcard for all secrets of that type.
func TestSetDefault_TypeWildcard(t *testing.T) {
	g := NewElevationGate(nil)
	g.SetDefault(SecretAllow, "API Key") // wildcard for vault keys starting with "API Key:"

	secrets := []DetectedSecret{
		{Type: "API Key", Snippet: "sk-wildcardtest9876543210abcdefghijklmnopq"},
	}
	action, err := g.Evaluate(secrets, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretAllow, action)
}

// TestEvaluate_NilPrompter_ErrorsHandled verifies that a nil prompter doesn't
// panic or return errors — just returns SecretRedact.
func TestEvaluate_NilPrompter_ErrorsHandled(t *testing.T) {
	g := NewElevationGate(nil)

	secrets := []DetectedSecret{
		{Type: "Env Var Value", Snippet: "my-nil-prompter-test-secret-value-here"},
	}
	action, err := g.Evaluate(secrets, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretRedact, action)
}

// TestVaultKey_Deduplication verifies that vaultKey produces the same key
// for the same type and snippet prefix.
func TestVaultKey_Deduplication(t *testing.T) {
	s1 := DetectedSecret{Type: "API Key", Snippet: "sk-abcdefghijklmnopqrstuvxyz01234567890"}
	s2 := DetectedSecret{Type: "API Key", Snippet: "sk-abcdefghijklmnopqrstuvxyz01234567890-extra-stuff"}
	s3 := DetectedSecret{Type: "Token", Snippet: "sk-abcdefghijklmnopqrstuvxyz01234567890"}

	// Same type + same first 8 chars of snippet → same vault key.
	assert.Equal(t, vaultKey(s1), vaultKey(s2))

	// Different type → different vault key.
	assert.NotEqual(t, vaultKey(s1), vaultKey(s3))
}

// TestEvaluate_PrompterError verifies that if the prompter returns an error,
// the gate safely falls back to SecretRedact.
func TestEvaluate_PrompterError(t *testing.T) {
	p := &mockPrompter{err: errors.New("prompting failed")}
	g := NewElevationGate(p)

	secrets := []DetectedSecret{
		{Type: "API Key", Snippet: "sk-prompterror9876543210abcdefghijklmnopqrst"},
	}
	action, err := g.Evaluate(secrets, "shell")
	assert.NoError(t, err) // Evaluate itself should not return the prompting error
	assert.Equal(t, SecretRedact, action)

	// The secret should still be cached (safe default).
	assert.Contains(t, g.vault, vaultKey(secrets[0]))
}

// TestEvaluate_DeduplicatesWithinBatch verifies that the same secret appearing
// twice in a single evaluation is only prompted once.
func TestEvaluate_DeduplicatesWithinBatch(t *testing.T) {
	callCount := 0
	g := NewElevationGate(&countingPrompter{count: &callCount, action: SecretAllow})

	s := DetectedSecret{Type: "API Key", Snippet: "sk-deduptest99887766554433221100998877665544"}
	action, err := g.Evaluate([]DetectedSecret{s, s}, "shell")
	assert.NoError(t, err)
	assert.Equal(t, SecretAllow, action)
	assert.Equal(t, 1, callCount, "prompt should only be called once for duplicate secrets")
}

// countingPrompter is a mockPrompter that tracks call count.
type countingPrompter struct {
	count  *int
	action SecretAction
}

func (c *countingPrompter) PromptSecretAction(_ []DetectedSecret, _ string) (SecretAction, error) {
	*c.count++
	return c.action, nil
}
