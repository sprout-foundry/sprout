// Package agent — SP-125 LCM auto-activation end-to-end tests for the CLI
// agent-creation path. The existing sp125_low_context_integration_test.go
// covers the NewAgentWithClient (WASM/SDK) path against a mock client;
// this file covers the production CLI path (newAgentWithConfigManagerInner)
// against a real GenericProvider pointed at an httptest server.
//
// Why this test exists:
//
// The CLI path uses newAgentWithConfigManagerInner, which historically
// loaded the system prompt via GetEmbeddedSystemPromptWithProvider — the
// NON-profile-aware loader that always returns the 6.6K full prompt.
// The NewAgentWithClient path used GetEmbeddedSystemPromptForProfile,
// which loads the 1.5K lite prompt when LCM is active. Same config, same
// model window, two different prompts depending on which constructor
// the caller used. LM Studio / Ollama users on 8K–64K context models
// saw full-sprout cost on top of the 8-tool allowlist — the worst of
// both worlds: LCM throttles the tools but the prompt still pays the
// full-sprout tax.
//
// The fix consolidates profile+system-prompt resolution into a single
// helper (resolveProfileAndSystemPrompt) called by both constructors.
// This test pins the CLI path end-to-end: build a real GenericProvider
// against an httptest server, route through resolveProfileAndSystemPrompt
// via the CLI constructor, and assert the lite prompt came out.
//
// What this test does NOT cover:
//
// We skip the production connection-check request (SPROUT_SKIP_CONNECTION_CHECK=1)
// because (a) under `go test` stdin is a pipe so isNonInteractive() is
// true and the connection-check failure routes into the non-interactive
// recovery path which is already tested elsewhere, and (b) we want
// profile resolution to be the variable under test, not the network
// round-trip. The httptest server is therefore only used to verify
// the GenericProvider constructor accepts our config; it does not
// validate the production connection-check flow.
//
// Test isolation:
//
// The GlobalFactory is a package-level singleton pre-loaded with
// embedded provider configs at factory init. There are TWO such
// singletons (one each in pkg/agent_providers and pkg/factory, both
// instantiated in their package's init()) and the production CLI
// path uses the one in pkg/factory. We save the original "lmstudio"
// config from BOTH singletons before each test and restore them via
// t.Cleanup so parallel tests (and the test that runs after ours)
// see the original fixture. Using "lmstudio" (auth.type="none")
// avoids the API-key resolution path entirely — the test doesn't
// need real credentials.
//
// Environment variables set per-test:
//
//   - ALLOW_REAL_PROVIDER: bypasses the isRunningUnderTest() shortcut
//     in newAgentWithConfigManagerInner that would otherwise route to
//     the TestClient. We need the real provideClient flow.
//   - SKIP_CONNECTION_CHECK: bypasses the production chat-completions
//     round-trip CheckConnection() does (see "What this test does NOT
//     cover" above). The httptest server is therefore not exercised
//     by the production path; it exists only to satisfy
//     GenericProvider construction.
//   - MODEL_REGISTRY_URL=off: prevents GenericProvider.GetModelContextLimit
//     from fetching the remote GitHub-Pages registry during the test
//     (would add up to 2s latency and risk of network flakiness).
//   - MODEL_REGISTRY_TIMEOUT=200ms: belt-and-braces fast timeout.
package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/factory"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// cliTestProviderName is the provider name used by the CLI-path tests
// below. "lmstudio" is chosen because (a) its embedded config declares
// auth.type="none", so EnsureAPIKey is a no-op and the test needs no
// real credentials, and (b) the factory's CreateProviderClient routes
// LMStudioClientType to CreateGenericProvider("lmstudio", model) — so
// upserting "lmstudio" in the global factory is enough to wire the
// test httptest server into the CLI path.
const cliTestProviderName = "lmstudio"

// cliTestModel is the model name the tests report to the GenericProvider.
// Distinct from "test-model" so it cannot collide with embedded configs,
// and chosen so it doesn't match any model_overrides / pattern_overrides
// in the test provider config — the path under test is the default
// context limit, not the model_overrides table.
const cliTestModel = "cli-lcm-test-model"

// assertPromptContainsBody asserts that the loaded system prompt
// contains the embedded profile-specific prompt body. AGENTS.md
// (and other concatenation suffixes) are appended after the body, so
// we use substring containment rather than equality. The "low" or
// "full" label is used in error messages to identify which profile
// is being asserted. We reuse extractSystemPromptForProfile so the
// comparison target is the same string slice the production helper
// concatenates — comparing against the raw markdown file would
// include the "Agent System Prompt (Lite …)" header that lives
// outside the triple-backtick fences, which is never sent to the
// model and would falsely fail the assertion.
func assertPromptContainsBody(t *testing.T, loaded string, profile configuration.ContextProfile, label string) {
	t.Helper()
	embedded, err := extractSystemPromptForProfile(profile)
	if err != nil {
		t.Fatalf("extract embedded prompt for %s profile: %v", label, err)
	}
	if !strings.Contains(loaded, strings.TrimSpace(embedded)) {
		const headChars = 200
		loadedHead := loaded
		if len(loadedHead) > headChars {
			loadedHead = loadedHead[:headChars] + "…"
		}
		t.Errorf("loaded system prompt does not contain the %s prompt body (loaded head: %q)", label, loadedHead)
	}
}

// buildCLIProviderConfigJSON constructs a minimal ProviderConfig JSON
// matching the structure of pkg/agent_providers/configs/lmstudio.json,
// with the caller-supplied default_context_limit. The model defaults
// to cliTestModel so the GenericProvider's GetModelContextLimit resolves
// to default_context_limit (no model_overrides are set, so the
// pattern_overrides and model_info lookups are skipped).
func buildCLIProviderConfigJSON(defaultContextLimit int) string {
	// We round-trip through DefaultContextLimit twice for the two
	// accepted fields: the legacy Models.ContextLimit and the
	// canonical Models.DefaultContextLimit. Keeping both is what
	// the openai.json/lmstudio.json fixtures do, and the failing
	// config validator (validateModelConfig) requires at least one
	// of them to be > 0.
	return fmt.Sprintf(`{
		"name": %q,
		"endpoint": "http://127.0.0.1:1/v1/chat/completions",
		"auth": {
			"type": "none",
			"env_var": ""
		},
		"headers": {},
		"defaults": {
			"model": %q,
			"temperature": 0.7,
			"max_tokens": -1,
			"top_p": 1.0
		},
		"message_conversion": {
			"include_tool_call_id": true,
			"convert_tool_role_to_user": false,
			"reasoning_content_field": ""
		},
		"streaming": {
			"format": "sse",
			"chunk_timeout_ms": 300000,
			"done_marker": "[DONE]"
		},
		"models": {
			"default_context_limit": %d,
			"context_limit": %d,
			"default_model": %q,
			"available_models": [%q],
			"supports_vision": false,
			"vision_model": ""
		},
		"retry": {
			"max_attempts": 3,
			"base_delay_ms": 1000,
			"backoff_multiplier": 2.0,
			"max_delay_ms": 30000,
			"retryable_errors": ["rate_limit_exceeded", "temporary_error", "timeout"]
		},
		"cost": {
			"input_token_cost": 0.0,
			"output_token_cost": 0.0,
			"currency": "USD"
		},
		"display_name": "CLI LCM Test"
	}`, cliTestProviderName, cliTestModel, defaultContextLimit, defaultContextLimit, cliTestModel, cliTestModel)
}

// chatCompletionsResponse is the minimal OpenAI-compatible chat completion
// payload the test httptest server returns. Every field is verified by
// GenericProvider.decodeChatResponseWithCost (id, object, model, choices,
// usage); keep the shape stable when changing this fixture.
func chatCompletionsResponse() string {
	return `{
		"id": "chatcmpl-cli-lcm-test",
		"object": "chat.completion",
		"created": 1,
		"model": "` + cliTestModel + `",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "ok"
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 1,
			"completion_tokens": 1,
			"total_tokens": 2
		}
	}`
}

// newCLIPathTestServer returns an httptest server that responds to
// /v1/chat/completions with a valid OpenAI-shaped completion payload.
// We only need the chat-completions endpoint — the test sets the model
// in the provider config so ensureModel() (the Goyer-provider's
// ListModels fallback) is never reached.
func newCLIPathTestServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(chatCompletionsResponse()))
	})
	return httptest.NewServer(mux)
}

// cliPathTestEnv prepares the global state required to drive
// newAgentWithConfigManagerInner end-to-end with a real GenericProvider.
//
// Returns:
//   - manager: an isolated configuration Manager pointing at a temp dir
//     (via NewTestManager) so the test cannot touch the user's real
//     ~/.config/sprout settings.
//   - server: the httptest server the GenericProvider will hit. The
//     caller's t.Cleanup-equivalent is the returned cleanup() func.
//   - cleanup: restores the original "lmstudio" config in the global
//     factory and removes the env vars the test set. ALWAYS defer
//     cleanup(); the next test in the package will see the original
//     factory only if every test cleans up after itself.
//
// Important: sprout has TWO independent global factory singletons
// (`pkg/agent_providers.GlobalFactory` and `pkg/factory.GlobalFactory`).
// factory.CreateGenericProvider (the path used by agent creation) reads
// from `pkg/factory.GlobalFactory`, while pkg/agent_providers' singleton
// is the older API. We must upsert into BOTH so the test config is
// honored end-to-end.
//
// Environment variables set by this helper:
//
//   - LEDIT_CONFIG / SPROUT_CONFIG: redirected to the temp dir via
//     NewTestManager (so any indirect Load() lands in the temp dir).
//   - ALLOW_REAL_PROVIDER: bypasses the isRunningUnderTest() shortcut
//     in newAgentWithConfigManagerInner that would route to TestClient.
//   - SKIP_CONNECTION_CHECK: bypasses CheckConnection()'s production
//     chat-completions round-trip so the variable under test is the
//     profile resolution, not the network round-trip. See the package-
//     level comment's "What this test does NOT cover" section for why
//     this seam is the right one to use.
//   - MODEL_REGISTRY_URL=off: GenericProvider's GetModelContextLimit
//     fetches from the remote GitHub-Pages registry; disabling it
//     keeps the test deterministic and avoids 2s network timeouts.
//   - MODEL_REGISTRY_TIMEOUT=200ms: belt-and-braces fast timeout in
//     case the registry URL is left enabled in some path.
//
// Both SPROUT_* and LEDIT_* prefixes are set for env vars that the
// codebase reads under either name (configuration.GetEnvSimple looks
// up both). This keeps the tests stable across the LEDIT→SPROUT
// rename and matches the pattern used elsewhere in pkg/agent_tests.
func cliPathTestEnv(t *testing.T, defaultContextLimit int) (manager *configuration.Manager, server *httptest.Server, cleanup func()) {
	t.Helper()

	// Step 1: isolated config dir so the test cannot touch the user's
	// real config. NewTestManager handles LEDIT_CONFIG/SPROUT_CONFIG
	// and the Layer-5 cleanup-detector that complains if the real
	// config gets modified.
	manager, mgrCleanup := configuration.NewTestManager(t)

	// Step 2: bypass the isRunningUnderTest() shortcut in
	// newAgentWithConfigManagerInner. Without this, the CLI path
	// would route to TestClientType and never exercise our helper.
	// GetEnvSimple checks SPROUT_ and LEDIT_ prefixes, so we set both
	// to be safe.
	t.Setenv("SPROUT_ALLOW_REAL_PROVIDER", "1")
	t.Setenv("LEDIT_ALLOW_REAL_PROVIDER", "1")
	// Skip the production connection-check request so the variable
	// under test is profile resolution, not the network round-trip.
	// See package-level comment's "What this test does NOT cover".
	t.Setenv("SPROUT_SKIP_CONNECTION_CHECK", "1")
	t.Setenv("LEDIT_SKIP_CONNECTION_CHECK", "1")
	// Disable the model registry fetch so GenericProvider.GetModelContextLimit
	// resolves through the config (default_context_limit) rather than
	// hitting the remote registry. The fetch has a 2s timeout and
	// returns Empty models when the registry 404s; both behaviors are
	// noisy and avoidable in tests.
	t.Setenv("SPROUT_MODEL_REGISTRY_URL", "off")
	t.Setenv("LEDIT_MODEL_REGISTRY_URL", "off")
	t.Setenv("SPROUT_MODEL_REGISTRY_TIMEOUT", "200ms")
	t.Setenv("LEDIT_MODEL_REGISTRY_TIMEOUT", "200ms")
	// Skip the connection probe. Under `go test` stdin is a pipe,
	// isNonInteractive() returns true, and any failed CheckConnection
	// is fatal via recoverProviderStartup. The LCM logic under test
	// doesn't depend on connectivity — it depends only on
	// GetModelContextLimit, which reads from the provider config we
	// just upserted. Skipping the probe keeps the test focused.
	t.Setenv("SPROUT_SKIP_CONNECTION_CHECK", "1")
	t.Setenv("LEDIT_SKIP_CONNECTION_CHECK", "1")

	// Step 3: write the provider config to a temp dir so the user
	// (and the failure mode where the provider config is loaded from
	// the filesystem) can be convinced the config is real. We don't
	// actually load from this file — the GlobalFactory is the
	// in-process registry — but the file proves the test is wired
	// against a syntactically valid provider config JSON.
	configDir := t.TempDir()
	configFile := filepath.Join(configDir, cliTestProviderName+".json")
	if err := os.WriteFile(configFile, []byte(buildCLIProviderConfigJSON(defaultContextLimit)), 0o600); err != nil {
		t.Fatalf("write provider config to %s: %v", configFile, err)
	}

	// Step 4: capture the original "lmstudio" config from BOTH factory
	// singletons so we can restore them after the test. Without this,
	// the next test that touches the factory would see our test
	// endpoint URL and (in CI) try to "ensure model" against a
	// long-dead httptest server. Each singleton has its own state
	// because pkg/agent_providers and pkg/factory each instantiate a
	// ProviderFactory and load embedded configs in their own init().
	originalProvidersLMStudio, originalProvidersErr := providers.GlobalFactory().GetProviderConfig(cliTestProviderName)
	originalFactoryLMStudio, originalFactoryErr := factory.GlobalFactory().GetProviderConfig(cliTestProviderName)

	// Step 5: build the test ProviderConfig and upsert it into BOTH
	// factories. The endpoint is rewritten to the running httptest
	// server URL. The parsed config is reused from the JSON file we
	// wrote above so the schema matches what real (filesystem)
	// configs look like.
	server = newCLIPathTestServer()
	buildTestConfig := func() (*providers.ProviderConfig, error) {
		data, err := os.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("read provider config: %w", err)
		}
		var cfg providers.ProviderConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse provider config: %w", err)
		}
		cfg.Endpoint = server.URL + "/v1/chat/completions"
		return &cfg, nil
	}

	cfg, err := buildTestConfig()
	if err != nil {
		t.Fatalf("build test config: %v", err)
	}
	if err := providers.GlobalFactory().UpsertConfig(cliTestProviderName, cfg); err != nil {
		t.Fatalf("upsert test provider config (pkg/agent_providers): %v", err)
	}
	if err := factory.GlobalFactory().UpsertConfig(cliTestProviderName, cfg); err != nil {
		t.Fatalf("upsert test provider config (pkg/factory): %v", err)
	}

	// Step 6: set the test provider as the active one in the test
	// config so newAgentWithConfigManagerInner's ResolveProviderModel
	// picks it up. We also set ProviderModels["lmstudio"] so the
	// model arg returned by ResolveProviderModel is non-empty (else
	// the GenericProvider falls back to ListModels discovery, which
	// the test server doesn't implement).
	if err := manager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = cliTestProviderName
		if cfg.ProviderModels == nil {
			cfg.ProviderModels = make(map[string]string)
		}
		cfg.ProviderModels[cliTestProviderName] = cliTestModel
		return nil
	}); err != nil {
		t.Fatalf("configure test provider as last-used: %v", err)
	}

	// restoreFactory restores a single factory's "lmstudio" entry to
	// its pre-test snapshot. If the factory had no entry (the rare
	// case where load ordering put lmstudio in only one of the two
	// factories) we leave the upsert in place — the test isn't the
	// cause of a missing entry, and a missing entry in both factories
	// would have failed earlier in buildTestConfig.
	restoreFactory := func(name string, snapshot *providers.ProviderConfig, snapErr error, upsert func(string, *providers.ProviderConfig) error) {
		if snapErr != nil || snapshot == nil {
			return // factory had no entry pre-test; leave whatever's there.
		}
		if err := upsert(name, snapshot); err != nil {
			t.Logf("WARNING: failed to restore original lmstudio config (%s): %v", name, err)
		}
	}

	cleanup = func() {
		// Stop the httptest server first so any lingering goroutines
		// don't try to use the connection after the test.
		if server != nil {
			server.Close()
		}
		restoreFactory("pkg/agent_providers", originalProvidersLMStudio, originalProvidersErr, providers.GlobalFactory().UpsertConfig)
		restoreFactory("pkg/factory", originalFactoryLMStudio, originalFactoryErr, factory.GlobalFactory().UpsertConfig)
		mgrCleanup()
	}
	return manager, server, cleanup
}

// TestCLIPath_LCM_AutoActivatesAt32K is the regression test for the SP-125
// bug: the CLI path used to load the 6.6K full prompt for a 32K model
// instead of the 1.5K lite prompt. After the fix, the CLI path shares
// the profile-aware loader with NewAgentWithClient, so a 32K context
// model in the CLI path now produces the lite prompt and an LCM profile.
//
// The test:
//  1. Stands up a real GenericProvider via httptest (so the
//     GenericProvider constructor accepts our config — see the
//     package-level comment's "What this test does NOT cover"
//     section for why we skip CheckConnection's network round-trip).
//  2. Calls newAgentWithConfigManagerInner with the test provider set
//     as LastUsedProvider.
//  3. Asserts the agent's context profile is LCM (auto-detected from
//     the 32K window).
//  4. Asserts the agent's system prompt contains the same body as the
//     embedded lite prompt — a content-based assertion that's stable
//     against AGENTS.md edits and future prompt tweaks.
func TestCLIPath_LCM_AutoActivatesAt32K(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 32_000)
	defer cleanup()

	// Workspace root is a server-side temp dir, NOT the user's $HOME —
	// autoActivateCoordinatorPersona would otherwise try to activate
	// the coordinator persona and we'd be testing the wrong thing.
	workspaceRoot := t.TempDir()

	ag, err := newAgentWithConfigManagerInner(manager, workspaceRoot, "")
	if err != nil {
		t.Fatalf("newAgentWithConfigManagerInner failed: %v", err)
	}
	defer ag.Shutdown()

	if ag.contextProfile.Mode != configuration.ContextModeLowContext {
		t.Errorf("expected ContextModeLowContext for 32K-context CLI path, got %q", ag.contextProfile.Mode)
	}

	// Compare the loaded prompt's body against the embedded lite
	// prompt's body. This is stable against AGENTS.md edits (which
	// inflate the total prompt tokens but are appended after the body)
	// and against future prompt tweaks (the body is read live from the
	// embedded file so the test updates with the prompt). The lite
	// prompt must contain the body — a marker substring is sufficient
	// and avoids brittleness from full-body equality (AGENTS.md and
	// configuration additions alter the loaded prompt's exact text).
	assertPromptContainsBody(t, ag.GetSystemPrompt(), ag.contextProfile, "lite")

	// Regression sanity: the 8-tool LCM allowlist should also be
	// active on the CLI path (proves the profile flowed all the way
	// through, not just the prompt).
	tools := ag.getOptimizedToolDefinitions(nil)
	lcmTools := map[string]bool{
		"shell_command": true, "read_file": true, "write_file": true,
		"edit_file": true, "search_files": true, "commit": true,
		"list_changes": true, "recover_file": true,
	}
	if len(tools) != len(lcmTools) {
		var names []string
		for _, tool := range tools {
			names = append(names, tool.Function.Name)
		}
		t.Errorf("CLI path should produce LCM 8-tool allowlist at 32K; got %d tools: %v", len(tools), names)
	}
}

// TestCLIPath_FullContextAt128K verifies the negative case: at 128K
// the CLI path should NOT activate LCM, the full prompt should be
// loaded, and the LCM marker should be absent. Same content-based
// body assertion as the 32K test, anchored against the embedded full
// prompt's body.
func TestCLIPath_FullContextAt128K(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 128_000)
	defer cleanup()

	workspaceRoot := t.TempDir()
	ag, err := newAgentWithConfigManagerInner(manager, workspaceRoot, "")
	if err != nil {
		t.Fatalf("newAgentWithConfigManagerInner failed: %v", err)
	}
	defer ag.Shutdown()

	if ag.contextProfile.Mode == configuration.ContextModeLowContext {
		t.Errorf("128K context should not activate LCM, got %q", ag.contextProfile.Mode)
	}

	assertPromptContainsBody(t, ag.GetSystemPrompt(), ag.contextProfile, "full")
}

// TestCLIPath_ContextFloor_4K verifies that the CLI path's
// profile-resolution error (the 8K floor) propagates through
// newAgentWithConfigManagerInner end-to-end. Same contract as
// TestSP125_ContextFloor_4K in sp125_low_context_integration_test.go,
// but exercised against the production CLI path so a regression that
// only affected the CLI helper would be caught here.
func TestCLIPath_ContextFloor_4K(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 4_096)
	defer cleanup()

	workspaceRoot := t.TempDir()
	_, err := newAgentWithConfigManagerInner(manager, workspaceRoot, "")
	if err == nil {
		t.Fatal("expected context-floor error for 4K context, got nil")
	}

	msg := err.Error()
	// The exact string from configuration.ResolveContextProfile's
	// floor error; the new helper wraps it via agenterrors.NewPermanentError
	// but the wrapped Message preserves the substring.
	if !strings.Contains(msg, "8000-token minimum") {
		t.Errorf("CLI path error should mention the 8000-token minimum, got: %s", msg)
	}
	if !strings.Contains(msg, "4096") {
		t.Errorf("CLI path error should mention the actual context window (4096), got: %s", msg)
	}
}

// TestCLIPath_LCM_HelperConcurrencySmoke is a low-cost sanity check
// that resolveProfileAndSystemPrompt doesn't race when CLI agent
// construction is invoked concurrently. The fix involves shared
// state (the same ResolveContextProfile call is now made by both
// constructors), so a regression that introduced a data race would
// surface here under `go test -race`.
//
// We run the CLI path in parallel goroutines and assert all complete
// without panic and produce the same LCM profile. The race detector
// is the actual assertion; the value checks are guard rails.
func TestCLIPath_LCM_HelperConcurrencySmoke(t *testing.T) {
	manager, _, cleanup := cliPathTestEnv(t, 32_000)
	defer cleanup()

	workspaceRoot := t.TempDir()

	const goroutines = 4
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	modes := make(chan configuration.ContextMode, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ag, err := newAgentWithConfigManagerInner(manager, workspaceRoot, "")
			if err != nil {
				errs <- fmt.Errorf("goroutine %d: %w", idx, err)
				return
			}
			defer ag.Shutdown()
			modes <- ag.contextProfile.Mode
		}(i)
	}
	wg.Wait()
	close(errs)
	close(modes)

	for err := range errs {
		t.Errorf("concurrent CLI path construction failed: %v", err)
	}
	for mode := range modes {
		if mode != configuration.ContextModeLowContext {
			t.Errorf("concurrent construction produced non-LCM mode %q", mode)
		}
	}
}
