//go:build !js

package webui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleAPICompletionMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/completion", nil)
	rec := httptest.NewRecorder()
	ws.handleAPICompletion(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPICompletionInvalidJSON(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/completion", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPICompletion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp["code"] != "invalid_json" {
		t.Fatalf("expected code invalid_json, got %v", resp["code"])
	}
}

func TestHandleAPICompletionEmptyPrefix(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/completion", strings.NewReader(`{"prefix":""}`))
	rec := httptest.NewRecorder()
	ws.handleAPICompletion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp["code"] != "prefix_required" {
		t.Fatalf("expected code prefix_required, got %v", resp["code"])
	}
}

// newCompletionTestManager creates an isolated configuration manager in a
// temp directory so tests never touch the caller's real config.
func newCompletionTestManager(t *testing.T) *configuration.Manager {
	t.Helper()
	mgr, err := configuration.NewManagerWithDir(t.TempDir())
	require.NoError(t, err)
	return mgr
}

// TestResolveCompletionClient_RoleCoderWins verifies that the completion
// endpoint resolves its LLM client through the completion getters, which
// fall back to the coder role: with only roles.coder set, the role's
// provider/model beat the conversation's last-used provider.
func TestResolveCompletionClient_RoleCoderWins(t *testing.T) {
	mgr := newCompletionTestManager(t)
	require.NoError(t, mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = string(api.OllamaClientType)
		cfg.Roles = map[string]configuration.RoleConfig{
			configuration.RoleCoder: {Provider: string(api.TestClientType), Model: "completion-role-model"},
		}
		return nil
	}))

	client, clientType, model, err := resolveCompletionClient(mgr)
	require.NoError(t, err)
	require.NotNil(t, client, "the coder role must resolve a client")
	assert.Equal(t, api.ClientType(api.TestClientType), clientType)
	assert.Equal(t, "completion-role-model", model)
}

// TestResolveCompletionClient_CompletionSettingsAliasCoderRole verifies the
// alias direction at the call site: the legacy completion settings win in
// the completion getters, so pre-role configurations keep working.
func TestResolveCompletionClient_CompletionSettingsAliasCoderRole(t *testing.T) {
	mgr := newCompletionTestManager(t)
	require.NoError(t, mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.CompletionProvider = string(api.TestClientType)
		cfg.CompletionModel = "legacy-completion-model"
		return nil
	}))

	client, clientType, model, err := resolveCompletionClient(mgr)
	require.NoError(t, err)
	require.NotNil(t, client, "the legacy completion settings must resolve a client via the completion getters")
	assert.Equal(t, api.ClientType(api.TestClientType), clientType)
	assert.Equal(t, "legacy-completion-model", model)
}

// TestResolveCompletionClient_NoSelectionUsesLastUsedProvider verifies the
// fallback shape: with no completion settings and no roles, the completion
// getters resolve empty, so the main conversation provider path (the
// conversation's last-used provider) is used.
func TestResolveCompletionClient_NoSelectionUsesLastUsedProvider(t *testing.T) {
	mgr := newCompletionTestManager(t)
	require.NoError(t, mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = string(api.TestClientType)
		return nil
	}))

	client, clientType, _, err := resolveCompletionClient(mgr)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, api.ClientType(api.TestClientType), clientType)
}

// TestResolveCompletionClient_SubagentSettingsDoNotLeak verifies the
// completion-first resolution: with only the subagent settings configured —
// the coder role's general alias, but NOT the completion path's — the
// completion resolution must use the main conversation provider, and the
// subagent model must not leak into the completion path. The subagent
// provider is a resolvable one (openai), so a resolver that consults the
// subagent settings would leak them here: the subagent's provider/model
// would win over the main provider.
func TestResolveCompletionClient_SubagentSettingsDoNotLeak(t *testing.T) {
	mgr := newCompletionTestManager(t)
	require.NoError(t, mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = string(api.TestClientType)
		cfg.SubagentProvider = string(api.OpenAIClientType)
		cfg.SubagentModel = "subagent-model"
		return nil
	}))

	client, clientType, model, err := resolveCompletionClient(mgr)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, api.ClientType(api.TestClientType), clientType, "the main conversation provider must resolve the client")
	assert.NotEqual(t, "subagent-model", model, "the subagent model must not leak into the completion path")
}

// TestResolveCompletionClient_UnresolvableProviderErrors verifies the main
// provider error path: an unresolvable last-used provider yields the
// failed_to_resolve_provider sentinel.
func TestResolveCompletionClient_UnresolvableProviderErrors(t *testing.T) {
	mgr := newCompletionTestManager(t)
	require.NoError(t, mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = "no-such-provider-xyz"
		return nil
	}))

	_, _, _, err := resolveCompletionClient(mgr)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errCompletionProviderUnresolvable), "expected the resolve-provider sentinel, got: %v", err)
}
