package configuration

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// NewTestManager builds a configuration Manager backed by an isolated
// temp directory so that tests never read, modify, or create files in
// the caller's real ~/.config/sprout config. Sets SPROUT_CONFIG +
// LEDIT_CONFIG to that temp dir via t.Setenv so any code path that
// uses GetConfigPath() (rather than the Manager) lands in the temp
// dir too.
//
// Returns the Manager and a cleanup func (no-op today; reserved for
// future hooks like Layer 5 detection). Callers should defer cleanup
// unconditionally to keep the contract stable as the helper grows.
//
// Usage:
//
//	mgr, cleanup := configuration.NewTestManager(t)
//	defer cleanup()
//
// This helper lives in a non-_test.go file so packages outside
// `configuration` can import it. Inside the configuration package it
// is treated like any other helper.
func NewTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()

	// Snapshot the REAL config path BEFORE we redirect env vars below,
	// so the Layer-5 cleanup detector compares against the right file.
	// Order is load-bearing: t.Setenv would change GetConfigDir's
	// answer and the detector would end up checking the temp dir,
	// which any normal Save in this helper would falsely fail.
	realDir, realDirErr := GetConfigDir()
	var realConfigPath string
	var realBefore []byte
	if realDirErr == nil {
		realConfigPath = filepath.Join(realDir, ConfigFileName)
		realBefore, _ = os.ReadFile(realConfigPath)
	}

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".sprout")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("NewTestManager: create temp config dir: %v", err)
	}

	// Scope BOTH env vars (SPROUT_CONFIG canonical, LEDIT_CONFIG legacy
	// alias) so any indirect Load() that bypasses our Manager still
	// lands in the temp dir. t.Setenv unwinds at test end.
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("LEDIT_CONFIG", configDir)

	mgr, err := NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewTestManager: NewManagerWithDir(%q): %v", configDir, err)
	}

	cleanup := func() {
		// Layer 5: verify the real config wasn't touched. Tests that
		// bypass the helper's isolation (e.g. by calling
		// configuration.Load() directly) will surface here instead of
		// silently corrupting the user's runtime config.
		if realDirErr != nil {
			return
		}
		realAfter, _ := os.ReadFile(realConfigPath)
		if string(realAfter) != string(realBefore) {
			t.Errorf("real config at %q was modified during test — "+
				"a Load()/Save() somewhere bypassed the isolation env. "+
				"Use the mgr returned by NewTestManager for all config "+
				"reads and writes, not configuration.Load().", realConfigPath)
		}
	}

	return mgr, cleanup
}

// sanitizeTestProvider strips the test-provider sentinel from a
// Config's LastUsedProvider field. Called at both Save (so we never
// write "test" to disk) and Load (so an already-poisoned config heals
// itself on the next CLI start).
//
// Background: `api.TestClientType` ("test") is an in-process sentinel
// for unit-test fixtures. It is NOT a valid runtime provider — the
// mock client returns canned responses regardless of input. Before
// this guard, leaky tests could write the literal string to the user's
// real config file (via cfg.Save() bypassing Manager.SetProvider's
// type-check). The next CLI run would pick it up and route /commit,
// chat, etc. to the mock — silently broken.
//
// Defense in depth: SetProvider, SetModelForProvider, Save, SaveToDir,
// Load, and LoadConfigWithLayers all funnel through this helper. New
// persist/load entry points should call it too. A new leak vector
// would have to invent a fresh persistence path AND skip this helper.
func sanitizeTestProvider(c *Config) {
	if c == nil {
		return
	}
	if c.LastUsedProvider == string(api.TestClientType) {
		log.Printf("[config] sanitizing test-provider sentinel from LastUsedProvider")
		c.LastUsedProvider = ""
	}
	// SubagentProvider mirrors LastUsedProvider for autonomous fleet
	// workers — same poisoning risk, same fix.
	if c.SubagentProvider == string(api.TestClientType) {
		log.Printf("[config] sanitizing test-provider sentinel from SubagentProvider")
		c.SubagentProvider = ""
	}
	// CommitProvider and ReviewProvider are resolved independently of
	// LastUsedProvider. If either holds the test sentinel, the commit
	// or review tool would route LLM calls to the mock client, which
	// returns "test" for every prompt — producing the literal string
	// as the generated commit message.
	if c.CommitProvider == string(api.TestClientType) {
		log.Printf("[config] sanitizing test-provider sentinel from CommitProvider")
		c.CommitProvider = ""
	}
	if c.ReviewProvider == string(api.TestClientType) {
		log.Printf("[config] sanitizing test-provider sentinel from ReviewProvider")
		c.ReviewProvider = ""
	}
	// ProviderModels is keyed by provider name. A "test" entry is
	// harmless on its own (nothing reads it once LastUsedProvider is
	// cleared) but contributes to file bloat over time; drop it.
	if _, ok := c.ProviderModels[string(api.TestClientType)]; ok {
		delete(c.ProviderModels, string(api.TestClientType))
	}
}
