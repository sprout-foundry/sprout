package credentials

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 1. parseKeyArray tests
// ---------------------------------------------------------------------------

func TestParseKeyArray(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantKey string
		wantErr bool
	}{
		{"empty string", "", 0, "", false},
		{"plain string", "sk-abc123", 1, "sk-abc123", false},
		{"JSON array 1 key", `["sk-only"]`, 1, "sk-only", false},
		{"JSON array 3 keys", `["sk-a","sk-b","sk-c"]`, 3, "sk-a", false},
		{"invalid JSON array", `[not valid json]`, 1, "[not valid json]", false},
		{"whitespace plain", "  sk-trimmed  ", 1, "sk-trimmed", false},
		{"whitespace JSON", `  ["sk-x", "sk-y"]  `, 2, "sk-x", false},
		{"just whitespace", "   ", 0, "", false},
		{"special chars", `["sk-abc_123+=/","key with spaces","日本語"]`, 3, "sk-abc_123+=/", false},
		{"empty strings inside", `["sk-a","","sk-c"]`, 3, "sk-a", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool, err := parseKeyArray(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, pool)
			assert.Equal(t, tc.wantLen, len(pool.Keys))
			if tc.wantLen > 0 {
				assert.Equal(t, tc.wantKey, pool.Keys[0])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. isJSONArrayValue tests
// ---------------------------------------------------------------------------

func TestIsJSONArrayValue(t *testing.T) {
	tests := []struct{ input string; want bool }{
		{"plain string", false},
		{`["key1","key2"]`, true},
		{`  ["key1"]  `, true},
		{"", false},
		{"   ", false},
		{"sk-123", false},
		{`{"not":"array"}`, false},
		{`[]`, true},
		{"[", false},               // no closing bracket
		{"[]something", false},     // trailing chars after ]
		{`["nested",{"obj":true}]`, true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.want, isJSONArrayValue(tc.input))
		})
	}
}

// ---------------------------------------------------------------------------
// 3. serializeKeyArray tests
// ---------------------------------------------------------------------------

func TestSerializeKeyArray(t *testing.T) {
	tests := []struct {
		name     string
		keys     []string
		wantJSON string
	}{
		{"empty", []string{}, "[]"},
		{"single", []string{"sk-only"}, `["sk-only"]`},
		{"multiple", []string{"sk-a", "sk-b", "sk-c"}, `["sk-a","sk-b","sk-c"]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := serializeKeyArray(tc.keys)
			require.NoError(t, err)
			assert.Equal(t, tc.wantJSON, out)
		})
	}
}

// ---------------------------------------------------------------------------
// 4. KeyRotator unit tests (fresh instances, NOT DefaultRotator)
// ---------------------------------------------------------------------------

func TestKeyRotator_NextKey_EmptyPool(t *testing.T) {
	r := NewKeyRotator()
	assert.Equal(t, "", r.NextKey("p", &KeyPool{Keys: []string{}}))
	assert.Equal(t, "", r.NextKey("p", nil))
}

func TestKeyRotator_NextKey_SingleKey(t *testing.T) {
	r := NewKeyRotator()
	pool := &KeyPool{Keys: []string{"sk-always"}}
	for i := 0; i < 5; i++ {
		assert.Equal(t, "sk-always", r.NextKey("p", pool), "iteration %d", i)
	}
}

func TestKeyRotator_NextKey_RoundRobin(t *testing.T) {
	r := NewKeyRotator()
	pool := &KeyPool{Keys: []string{"sk-a", "sk-b", "sk-c"}}
	expected := []string{"sk-a", "sk-b", "sk-c", "sk-a", "sk-b", "sk-c"}
	for i, want := range expected {
		assert.Equal(t, want, r.NextKey("p", pool), "iteration %d", i)
	}
}

func TestKeyRotator_Advance(t *testing.T) {
	r := NewKeyRotator()
	pool := &KeyPool{Keys: []string{"sk-a", "sk-b", "sk-c"}}
	assert.Equal(t, "sk-a", r.NextKey("p", pool))
	r.Advance("p") // skip sk-b
	assert.Equal(t, "sk-c", r.NextKey("p", pool))
}

func TestKeyRotator_Reset(t *testing.T) {
	r := NewKeyRotator()
	pool := &KeyPool{Keys: []string{"sk-a", "sk-b"}}
	r.NextKey("p", pool)
	r.NextKey("p", pool)
	r.Reset("p")
	assert.Equal(t, "sk-a", r.NextKey("p", pool))
}

func TestKeyRotator_CurrentIndex(t *testing.T) {
	r := NewKeyRotator()
	pool := &KeyPool{Keys: []string{"sk-a", "sk-b"}}

	// Unknown provider → -1
	assert.Equal(t, -1, r.CurrentIndex("unknown"))

	// After use → counter is advanced
	r.NextKey("p", pool)
	assert.Equal(t, 1, r.CurrentIndex("p"))
}

func TestKeyRotator_ConcurrentAccess(t *testing.T) {
	r := NewKeyRotator()
	pool := &KeyPool{Keys: []string{"sk-a", "sk-b", "sk-c"}}
	valid := []string{"sk-a", "sk-b", "sk-c"}
	var wg sync.WaitGroup
	results := make(chan string, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- r.NextKey("c", pool) }()
	}
	wg.Wait()
	close(results)
	for got := range results {
		assert.Contains(t, valid, got)
	}
}

func TestKeyRotator_FairDistribution(t *testing.T) {
	r := NewKeyRotator()
	pool := &KeyPool{Keys: []string{"sk-a", "sk-b", "sk-c"}}
	counts := map[string]int{}
	for i := 0; i < 300; i++ {
		counts[r.NextKey("d", pool)]++
	}
	assert.Equal(t, 100, counts["sk-a"])
	assert.Equal(t, 100, counts["sk-b"])
	assert.Equal(t, 100, counts["sk-c"])
}

func TestKeyRotator_MultipleProviders(t *testing.T) {
	r := NewKeyRotator()
	pA := &KeyPool{Keys: []string{"a1", "a2"}}
	pB := &KeyPool{Keys: []string{"b1", "b2", "b3"}}
	assert.Equal(t, "a1", r.NextKey("a", pA))
	assert.Equal(t, "b1", r.NextKey("b", pB))
	assert.Equal(t, "a2", r.NextKey("a", pA))
	assert.Equal(t, "b2", r.NextKey("b", pB))
	assert.Equal(t, "a1", r.NextKey("a", pA))
	assert.Equal(t, "b3", r.NextKey("b", pB))
}

func TestDefaultRotator_Isolation(t *testing.T) {
	r := NewKeyRotator()
	r.NextKey("iso", &KeyPool{Keys: []string{"sk-a"}})
	assert.Equal(t, -1, DefaultRotator.CurrentIndex("iso"))
}

// ---------------------------------------------------------------------------
// 5. KeyPool file backend integration tests
// ---------------------------------------------------------------------------

func setupFileBackend(t *testing.T) {
	t.Helper()
	t.Setenv("LEDIT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend()
}

func TestLoadKeyPool_NonExistentProvider(t *testing.T) {
	setupFileBackend(t)
	result, err := LoadKeyPool("no-such")
	require.NoError(t, err)
	assert.Empty(t, result.Pool.Keys)
}

func TestSaveAndLoad_SingleKey_PlainString(t *testing.T) {
	setupFileBackend(t)
	require.NoError(t, SaveKeyPool("openai", &KeyPool{Keys: []string{"sk-single"}}))

	// Verify backward-compat plain string format
	store, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "sk-single", store["openai"])

	// Verify round-trip
	result, err := LoadKeyPool("openai")
	require.NoError(t, err)
	assert.Equal(t, []string{"sk-single"}, result.Pool.Keys)
}

func TestSaveAndLoad_MultipleKeys_JSONArray(t *testing.T) {
	setupFileBackend(t)
	require.NoError(t, SaveKeyPool("openrouter", &KeyPool{Keys: []string{"sk-a", "sk-b", "sk-c"}}))

	store, err := Load()
	require.NoError(t, err)
	assert.True(t, isJSONArrayValue(store["openrouter"]))

	result, err := LoadKeyPool("openrouter")
	require.NoError(t, err)
	assert.Equal(t, []string{"sk-a", "sk-b", "sk-c"}, result.Pool.Keys)
}

func TestSaveAndLoad_LargePool(t *testing.T) {
	setupFileBackend(t)
	keys := make([]string, 50)
	for i := range keys {
		keys[i] = fmt.Sprintf("sk-key-%04d", i)
	}
	require.NoError(t, SaveKeyPool("big", &KeyPool{Keys: keys}))
	result, err := LoadKeyPool("big")
	require.NoError(t, err)
	assert.Equal(t, keys, result.Pool.Keys)
}

func TestAddKeyToPool(t *testing.T) {
	setupFileBackend(t)
	require.NoError(t, AddKeyToPool("ds", "sk-first"))

	result, err := LoadKeyPool("ds")
	require.NoError(t, err)
	assert.Equal(t, []string{"sk-first"}, result.Pool.Keys)

	// Add second key → converts to JSON array
	require.NoError(t, AddKeyToPool("ds", "sk-second"))
	result, err = LoadKeyPool("ds")
	require.NoError(t, err)
	assert.Equal(t, []string{"sk-first", "sk-second"}, result.Pool.Keys)

	// Duplicate rejected
	err = AddKeyToPool("ds", "sk-first")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestAddKeyToPool_TrimWhitespace(t *testing.T) {
	setupFileBackend(t)
	require.NoError(t, AddKeyToPool("o", "  sk-t  "))
	result, err := LoadKeyPool("o")
	require.NoError(t, err)
	assert.Equal(t, []string{"sk-t"}, result.Pool.Keys)

	err = AddKeyToPool("o", " sk-t ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestRemoveKeyFromPool(t *testing.T) {
	setupFileBackend(t)
	require.NoError(t, SaveKeyPool("m", &KeyPool{Keys: []string{"sk-a", "sk-b", "sk-c"}}))

	// Remove middle key
	require.NoError(t, RemoveKeyFromPool("m", "sk-b"))
	result, _ := LoadKeyPool("m")
	assert.Equal(t, []string{"sk-a", "sk-c"}, result.Pool.Keys)

	// Remove down to 1 → reverts to plain string
	require.NoError(t, RemoveKeyFromPool("m", "sk-c"))
	store, _ := Load()
	assert.False(t, isJSONArrayValue(store["m"]))

	// Remove last key → entry deleted
	require.NoError(t, RemoveKeyFromPool("m", "sk-a"))
	result, _ = LoadKeyPool("m")
	assert.Empty(t, result.Pool.Keys)
}

func TestRemoveKeyFromPool_Errors(t *testing.T) {
	setupFileBackend(t)

	// Empty pool
	err := RemoveKeyFromPool("d", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no keys found")

	// Key not in pool
	require.NoError(t, SaveKeyPool("d", &KeyPool{Keys: []string{"sk-exists"}}))
	err = RemoveKeyFromPool("d", "sk-different")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

func TestGetPoolSize(t *testing.T) {
	setupFileBackend(t)
	size, err := GetPoolSize("none")
	require.NoError(t, err)
	assert.Equal(t, 0, size)

	require.NoError(t, SaveKeyPool("o", &KeyPool{Keys: []string{"a", "b", "c"}}))
	size, err = GetPoolSize("o")
	require.NoError(t, err)
	assert.Equal(t, 3, size)
}

// ---------------------------------------------------------------------------
// Validation edge cases
// ---------------------------------------------------------------------------

func TestSaveKeyPool_Validation(t *testing.T) {
	setupFileBackend(t)
	require.Error(t, SaveKeyPool("", &KeyPool{Keys: []string{"x"}}))
	require.Error(t, SaveKeyPool("o", nil))
}

func TestAddRemove_Validation(t *testing.T) {
	setupFileBackend(t)
	require.Error(t, AddKeyToPool("", "x"))
	require.Error(t, AddKeyToPool("o", ""))
	require.Error(t, RemoveKeyFromPool("", "x"))
	require.Error(t, RemoveKeyFromPool("o", ""))
}

func TestLoadKeyPool_EmptyProvider(t *testing.T) {
	result, err := LoadKeyPool("")
	require.NoError(t, err)
	assert.Empty(t, result.Pool.Keys)
}

func TestRemoveKeyFromPoolByIndex(t *testing.T) {
	setupFileBackend(t)
	require.NoError(t, SaveKeyPool("idx", &KeyPool{Keys: []string{"sk-a", "sk-b", "sk-c"}}))

	// Remove middle key (index 1)
	require.NoError(t, RemoveKeyFromPoolByIndex("idx", 1))
	result, err := LoadKeyPool("idx")
	require.NoError(t, err)
	assert.Equal(t, []string{"sk-a", "sk-c"}, result.Pool.Keys)

	// Remove first key (index 0)
	require.NoError(t, RemoveKeyFromPoolByIndex("idx", 0))
	result, err = LoadKeyPool("idx")
	require.NoError(t, err)
	assert.Equal(t, []string{"sk-c"}, result.Pool.Keys)

	// Remove last key
	require.NoError(t, RemoveKeyFromPoolByIndex("idx", 0))
	result, err = LoadKeyPool("idx")
	require.NoError(t, err)
	assert.Empty(t, result.Pool.Keys)
}

func TestRemoveKeyFromPoolByIndex_Errors(t *testing.T) {
	setupFileBackend(t)

	// Negative index
	err := RemoveKeyFromPoolByIndex("idx", -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative")

	// Empty provider
	err = RemoveKeyFromPoolByIndex("", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider name cannot be empty")

	// Index out of bounds
	require.NoError(t, SaveKeyPool("idx", &KeyPool{Keys: []string{"sk-a"}}))
	err = RemoveKeyFromPoolByIndex("idx", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of bounds")

	// Non-existent provider
	err = RemoveKeyFromPoolByIndex("nope", 0)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// 6. Resolution with rotation integration (using resolve())
// ---------------------------------------------------------------------------

func TestResolve_SingleStoredKey(t *testing.T) {
	setupFileBackend(t)
	DefaultRotator.Reset("openai")
	t.Setenv("OPENAI_API_KEY", "") // clear real env var

	require.NoError(t, Save(Store{"openai": "sk-solo"}))
	ResetStorageBackend()

	for i := 0; i < 3; i++ {
		resolved, err := resolve("openai", "OPENAI_API_KEY")
		require.NoError(t, err, "iteration %d", i)
		assert.Equal(t, "sk-solo", resolved.Value, "iteration %d", i)
		assert.Equal(t, "stored", resolved.Source)
	}
}

func TestResolve_MultipleStoredKeys_Rotates(t *testing.T) {
	setupFileBackend(t)
	DefaultRotator.Reset("openrouter")
	t.Setenv("OPENROUTER_API_KEY", "")

	require.NoError(t, SaveKeyPool("openrouter", &KeyPool{Keys: []string{"sk-1", "sk-2", "sk-3"}}))
	ResetStorageBackend()

	for _, want := range []string{"sk-1", "sk-2", "sk-3", "sk-1", "sk-2"} {
		resolved, err := resolve("openrouter", "OPENROUTER_API_KEY")
		require.NoError(t, err)
		assert.Equal(t, want, resolved.Value)
		assert.Equal(t, "stored", resolved.Source)
	}

	// Reset + Advance: skip first key
	DefaultRotator.Reset("openrouter")
	DefaultRotator.Advance("openrouter")
	resolved, err := resolve("openrouter", "OPENROUTER_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "sk-2", resolved.Value)
}

func TestResolve_EnvVarOverridesStoredKeys(t *testing.T) {
	setupFileBackend(t)
	DefaultRotator.Reset("openai")

	t.Setenv("OPENAI_API_KEY", "")
	require.NoError(t, Save(Store{"openai": "sk-stored"}))
	ResetStorageBackend()

	t.Setenv("OPENAI_API_KEY", "sk-from-env")

	for i := 0; i < 3; i++ {
		resolved, err := resolve("openai", "OPENAI_API_KEY")
		require.NoError(t, err, "iteration %d", i)
		assert.Equal(t, "sk-from-env", resolved.Value, "iteration %d", i)
		assert.Equal(t, "environment", resolved.Source)
	}
}

// ---------------------------------------------------------------------------
// Convenience functions: GetNextKey, RotateKey
// ---------------------------------------------------------------------------

func TestGetNextKey(t *testing.T) {
	setupFileBackend(t)

	key, err := GetNextKey("none")
	require.NoError(t, err)
	assert.Empty(t, key)

	require.NoError(t, SaveKeyPool("o", &KeyPool{Keys: []string{"sk-1", "sk-2"}}))
	ResetStorageBackend()
	DefaultRotator.Reset("o")

	k1, _ := GetNextKey("o")
	k2, _ := GetNextKey("o")
	assert.Equal(t, "sk-1", k1)
	assert.Equal(t, "sk-2", k2)
}

func TestRotateKey(t *testing.T) {
	setupFileBackend(t)
	require.NoError(t, SaveKeyPool("d", &KeyPool{Keys: []string{"sk-x", "sk-y", "sk-z"}}))
	ResetStorageBackend()
	DefaultRotator.Reset("d")

	k, _ := GetNextKey("d")
	assert.Equal(t, "sk-x", k)

	RotateKey("d")
	k, _ = GetNextKey("d")
	assert.Equal(t, "sk-z", k)
}
