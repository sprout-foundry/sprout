package webui

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helper — builds a ReactWebServer with a live agent and config manager
// backed by a temporary directory.
// ---------------------------------------------------------------------------

func setupMCPCredTestServer(t *testing.T) (*ReactWebServer, string) {
	t.Helper()

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")

	// Reset the credential backend so the env var takes effect.
	credentials.ResetStorageBackend()

	daemonRoot := t.TempDir()
	workspaceDir := filepath.Join(daemonRoot, "workspace")
	require.NoError(t, os.MkdirAll(workspaceDir, 0o755))

	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = daemonRoot
	ws.terminalManager = NewTerminalManager(daemonRoot)
	ws.fileConsents = newFileConsentManager()

	clientID := "test-client"
	_, err := ws.setClientWorkspaceRoot(clientID, workspaceDir)
	require.NoError(t, err)

	return ws, tmpDir
}

// makeCredRequest builds an HTTP request with the JSON body and the test
// client ID header so that resolveClientID returns the right client.
func makeCredRequest(t *testing.T, method, path string, body interface{}) *http.Request {
	t.Helper()
	var reqBody *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewReader(data)
	} else {
		reqBody = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set(webClientIDHeader, "test-client")
	req.Header.Set("Content-Type", "application/json")
	return req
}

// decodeJSON is a test helper that decodes the recorder body into v.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), v))
}

// getConfigManager is a test helper that returns the config manager for the
// test client.
func getConfigManager(t *testing.T, ws *ReactWebServer) *configuration.Manager {
	t.Helper()
	agentInst, err := ws.getClientAgent("test-client")
	if err != nil && errors.Is(err, ErrNoProviderConfigured) {
		// If no provider is configured, create a config manager directly.
		cm, createErr := configuration.NewManagerSilent()
		require.NoError(t, createErr)
		return cm
	}
	require.NoError(t, err)
	require.NotNil(t, agentInst)
	cm := agentInst.GetConfigManager()
	require.NotNil(t, cm)
	return cm
}

// seedMCPServer adds an MCP server to the config so tests can operate on it.
func seedMCPServer(t *testing.T, ws *ReactWebServer, server mcp.MCPServerConfig) {
	t.Helper()
	cm := getConfigManager(t, ws)
	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.MCP.Servers == nil {
			cfg.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		cfg.MCP.Servers[server.Name] = server
		return nil
	})
	require.NoError(t, err)
}

// storeCredential directly writes a credential value to the file backend.
func storeCredential(t *testing.T, key, value string) {
	t.Helper()
	err := credentials.SetToActiveBackend(key, value)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// GET /api/settings/mcp/servers/{name}/credentials — handleGetServerCredentials
// ---------------------------------------------------------------------------

func TestGetServerCredentials_ServerNotFound(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	req := makeCredRequest(t, http.MethodGet, "/api/settings/mcp/servers/nonexistent/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "not found")
}

func TestGetServerCredentials_EmptyCredentialsMap(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
	})

	req := makeCredRequest(t, http.MethodGet, "/api/settings/mcp/servers/myserver/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getServerCredentialsResponse
	decodeJSON(t, rec, &resp)
	assert.Equal(t, "myserver", resp.Server)
	assert.Empty(t, resp.Credentials)
}

func TestGetServerCredentials_StatusSetAndMissing(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Create a server with two credential placeholders.
	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
		Credentials: map[string]string{
			"API_KEY":    mcp.SecretRef("myserver", "API_KEY"),
			"DB_PASSWORD": mcp.SecretRef("myserver", "DB_PASSWORD"),
		},
	})

	// Store only one of the two credentials in the backend.
	storeCredential(t, mcp.CredentialKey("myserver", "API_KEY"), "secret-key-123")

	req := makeCredRequest(t, http.MethodGet, "/api/settings/mcp/servers/myserver/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getServerCredentialsResponse
	decodeJSON(t, rec, &resp)
	assert.Equal(t, "myserver", resp.Server)
	assert.Len(t, resp.Credentials, 2)

	// API_KEY should be "set" because we stored it.
	assert.Equal(t, "set", resp.Credentials["API_KEY"].Status)
	assert.True(t, resp.Credentials["API_KEY"].HasValue)

	// DB_PASSWORD should be "missing" because we never stored it.
	assert.Equal(t, "missing", resp.Credentials["DB_PASSWORD"].Status)
	assert.False(t, resp.Credentials["DB_PASSWORD"].HasValue)
}

func TestGetServerCredentials_MissingServerName(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	req := makeCredRequest(t, http.MethodGet, "/api/settings/mcp/servers//credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// PUT /api/settings/mcp/servers/{name}/credentials — handlePutServerCredentials
// ---------------------------------------------------------------------------

func TestPutServerCredentials_Success(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
	})

	body := map[string]interface{}{
		"credentials": map[string]string{
			"API_KEY":    "my-secret-key",
			"DB_PASSWORD": "my-db-pass",
		},
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/mcp/servers/myserver/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.True(t, resp["success"].(bool))
	assert.Equal(t, "myserver", resp["server"])

	// Verify the credential was stored in the backend.
	val, _, err := credentials.GetFromActiveBackend(mcp.CredentialKey("myserver", "API_KEY"))
	require.NoError(t, err)
	assert.Equal(t, "my-secret-key", val)

	val, _, err = credentials.GetFromActiveBackend(mcp.CredentialKey("myserver", "DB_PASSWORD"))
	require.NoError(t, err)
	assert.Equal(t, "my-db-pass", val)

	// Verify the config was updated with secret ref placeholders.
	cm := getConfigManager(t, ws)
	cfg := cm.GetConfig()
	server := cfg.MCP.Servers["myserver"]
	require.NotNil(t, server.Credentials)
	assert.True(t, mcp.IsSecretRef(server.Credentials["API_KEY"]))
	assert.True(t, mcp.IsSecretRef(server.Credentials["DB_PASSWORD"]))
}

func TestPutServerCredentials_ServerNotFound(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	body := map[string]interface{}{
		"credentials": map[string]string{
			"API_KEY": "my-secret-key",
		},
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/mcp/servers/nonexistent/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "not found")
}

func TestPutServerCredentials_EmptyCredentialsMap(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
	})

	body := map[string]interface{}{
		"credentials": map[string]string{},
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/mcp/servers/myserver/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "cannot be empty")
}

func TestPutServerCredentials_InvalidEnvVarNames(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
	})

	body := map[string]interface{}{
		"credentials": map[string]string{
			"123-invalid": "some-value",
		},
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/mcp/servers/myserver/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "invalid credential name")
}

func TestPutServerCredentials_EnablesMCP(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Create server with MCP disabled.
	cm := getConfigManager(t, ws)
	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.MCP.Enabled = false
		if cfg.MCP.Servers == nil {
			cfg.MCP.Servers = make(map[string]mcp.MCPServerConfig)
		}
		cfg.MCP.Servers["myserver"] = mcp.MCPServerConfig{
			Name:    "myserver",
			Type:    "stdio",
			Command: "node",
			Args:    []string{"server.js"},
		}
		return nil
	})
	require.NoError(t, err)

	body := map[string]interface{}{
		"credentials": map[string]string{
			"API_KEY": "secret",
		},
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/mcp/servers/myserver/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// After putting credentials, MCP should be enabled.
	cfg := cm.GetConfig()
	assert.True(t, cfg.MCP.Enabled)
}

func TestPutServerCredentials_MigratesFromEnv(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Create server with credential stored in Env (old format).
	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
		Env: map[string]string{
			"API_KEY": "old-direct-value",
		},
	})

	body := map[string]interface{}{
		"credentials": map[string]string{
			"API_KEY": "new-secret-value",
		},
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/mcp/servers/myserver/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// The credential should be stored in the backend.
	val, _, err := credentials.GetFromActiveBackend(mcp.CredentialKey("myserver", "API_KEY"))
	require.NoError(t, err)
	assert.Equal(t, "new-secret-value", val)

	// The Env entry should have been removed (migrated).
	cfg := getConfigManager(t, ws).GetConfig()
	server := cfg.MCP.Servers["myserver"]
	_, existsInEnv := server.Env["API_KEY"]
	assert.False(t, existsInEnv, "API_KEY should be removed from Env after migration")
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/mcp/servers/{name}/credentials — handleDeleteServerCredentials
// ---------------------------------------------------------------------------

func TestDeleteServerCredentials_Success(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
		Credentials: map[string]string{
			"API_KEY": mcp.SecretRef("myserver", "API_KEY"),
		},
	})

	// Store the credential in the backend.
	storeCredential(t, mcp.CredentialKey("myserver", "API_KEY"), "secret-value")

	body := map[string]interface{}{
		"credential_name": "API_KEY",
	}
	req := makeCredRequest(t, http.MethodDelete, "/api/settings/mcp/servers/myserver/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.True(t, resp["success"].(bool))
	assert.Equal(t, "myserver", resp["server"])
	assert.Equal(t, "API_KEY", resp["deleted_credential"])

	// Verify the credential was removed from the backend.
	val, _, err := credentials.GetFromActiveBackend(mcp.CredentialKey("myserver", "API_KEY"))
	require.NoError(t, err)
	assert.Empty(t, val)

	// Verify it was removed from the server's Credentials map.
	cfg := getConfigManager(t, ws).GetConfig()
	server := cfg.MCP.Servers["myserver"]
	_, exists := server.Credentials["API_KEY"]
	assert.False(t, exists)
}

func TestDeleteServerCredentials_MissingCredential(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
	})

	// Deleting a credential that was never stored should still succeed gracefully.
	body := map[string]interface{}{
		"credential_name": "API_KEY",
	}
	req := makeCredRequest(t, http.MethodDelete, "/api/settings/mcp/servers/myserver/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.True(t, resp["success"].(bool))
}

func TestDeleteServerCredentials_ServerNotFound(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	body := map[string]interface{}{
		"credential_name": "API_KEY",
	}
	req := makeCredRequest(t, http.MethodDelete, "/api/settings/mcp/servers/nonexistent/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "not found")
}

func TestDeleteServerCredentials_EmptyCredentialName(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
	})

	body := map[string]interface{}{
		"credential_name": "",
	}
	req := makeCredRequest(t, http.MethodDelete, "/api/settings/mcp/servers/myserver/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "credential_name is required")
}

func TestDeleteServerCredentials_InvalidCredentialName(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
	})

	body := map[string]interface{}{
		"credential_name": "123-bad-name",
	}
	req := makeCredRequest(t, http.MethodDelete, "/api/settings/mcp/servers/myserver/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "invalid credential name")
}

func TestDeleteServerCredentials_AlsoRemovesFromEnv(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Seed server with credential in both Env and Credentials.
	seedMCPServer(t, ws, mcp.MCPServerConfig{
		Name:    "myserver",
		Type:    "stdio",
		Command: "node",
		Args:    []string{"server.js"},
		Credentials: map[string]string{
			"API_KEY": mcp.SecretRef("myserver", "API_KEY"),
		},
		Env: map[string]string{
			"API_KEY": "direct-value-in-env",
		},
	})

	body := map[string]interface{}{
		"credential_name": "API_KEY",
	}
	req := makeCredRequest(t, http.MethodDelete, "/api/settings/mcp/servers/myserver/credentials", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsMCPServers(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	cfg := getConfigManager(t, ws).GetConfig()
	server := cfg.MCP.Servers["myserver"]

	// Should be removed from both Credentials and Env.
	_, inCreds := server.Credentials["API_KEY"]
	_, inEnv := server.Env["API_KEY"]
	assert.False(t, inCreds, "API_KEY should be removed from Credentials")
	assert.False(t, inEnv, "API_KEY should be removed from Env (defense-in-depth)")
}

// ---------------------------------------------------------------------------
// extractServerNameFromCredentialsPath
// ---------------------------------------------------------------------------

func TestExtractServerNameFromCredentialsPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/settings/mcp/servers/myserver/credentials", "myserver"},
		{"/api/settings/mcp/servers/test-server/credentials", "test-server"},
		{"/api/settings/mcp/servers/a_b_c/credentials", "a_b_c"},
		// Without /credentials suffix (shouldn't normally happen for credential endpoints)
		{"/api/settings/mcp/servers/someserver", "someserver"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractServerNameFromCredentialsPath(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}
