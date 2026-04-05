// Tests for cmd/github_setup_prompt.go
package cmd

import (
	"bufio"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/mcp"
)

// =============================================================================
// Mock types for testing
// =============================================================================

// mockConfigManager simulates a configuration manager for testing
type mockConfigManager struct {
	config    *configuration.Config
	updateErr error
}

func (m *mockConfigManager) GetConfig() *configuration.Config {
	return m.config
}

func (m *mockConfigManager) UpdateConfig(updateFunc func(c *configuration.Config) error) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	return updateFunc(m.config)
}

// configManagerWithOverride allows overriding UpdateConfig for testing
type configManagerWithOverride struct {
	mockConfigManager *mockConfigManager
	updateFn        func(func(c *configuration.Config) error) error
}

func (m *configManagerWithOverride) GetConfig() *configuration.Config {
	return m.mockConfigManager.config
}

func (m *configManagerWithOverride) UpdateConfig(updateFunc func(c *configuration.Config) error) error {
	if m.updateFn != nil {
		return m.updateFn(updateFunc)
	}
	return m.mockConfigManager.UpdateConfig(updateFunc)
}

// mockAgent implements GitHubSetupAgentInterface for testing
type mockAgent struct {
	configManager    interface {
		GetConfig() *configuration.Config
		UpdateConfig(func(c *configuration.Config) error) error
	}
	mcpToolsRefreshed bool
	refreshErr       error
}

func (m *mockAgent) GetConfigManager() interface {
	GetConfig() *configuration.Config
	UpdateConfig(func(c *configuration.Config) error) error
} {
	return m.configManager
}

func (m *mockAgent) RefreshMCPTools() error {
	m.mcpToolsRefreshed = true
	return m.refreshErr
}

// =============================================================================
// Test cases for promptGitHubMCPSetupIfNeeded - Early return paths
// =============================================================================

func TestPromptGitHubMCPSetupIfNeeded_NilConfig(t *testing.T) {
	// Test: When config is nil, should return early without prompting
	mockCfgMgr := &mockConfigManager{config: nil}
	mockAgt := &mockAgent{configManager: mockCfgMgr}

	// This should not panic and should return early
	promptGitHubMCPSetupIfNeeded(mockAgt)
}

func TestPromptGitHubMCPSetupIfNeeded_SkipPromptTrue(t *testing.T) {
	// Test: When config.SkipPrompt is true, should return early
	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt: true,
			MCP:        mcp.MCPConfig{},
		},
	}
	mockAgt := &mockAgent{configManager: mockCfgMgr}

	promptGitHubMCPSetupIfNeeded(mockAgt)
}

func TestPromptGitHubMCPSetupIfNeeded_GetwdError(t *testing.T) {
	// Save original getwd and restore after test
	originalGetwd := getwdFunc
	getwdCalled := false
	getwdFunc = func() (string, error) {
		getwdCalled = true
		return "", errors.New("mock getwd error")
	}
	defer func() { getwdFunc = originalGetwd }()

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt: false,
			MCP:        mcp.MCPConfig{},
		},
	}
	mockAgt := &mockAgent{configManager: mockCfgMgr}

	promptGitHubMCPSetupIfNeeded(mockAgt)

	if !getwdCalled {
		t.Error("expected getwdFunc to be called")
	}
}

func TestPromptGitHubMCPSetupIfNeeded_ShouldPromptFalse(t *testing.T) {
	// Save original functions
	originalGetwd := getwdFunc
	originalShouldPrompt := shouldPromptGitHubSetup

	getwdCalled := false
	getwdFunc = func() (string, error) {
		getwdCalled = true
		return "/test/dir", nil
	}

	shouldPromptCalled := false
	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		shouldPromptCalled = true
		return false // Should not prompt
	}

	defer func() {
		getwdFunc = originalGetwd
		shouldPromptGitHubSetup = originalShouldPrompt
	}()

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt: false,
			MCP:        mcp.MCPConfig{},
		},
	}
	mockAgt := &mockAgent{configManager: mockCfgMgr}

	promptGitHubMCPSetupIfNeeded(mockAgt)

	if !getwdCalled {
		t.Error("expected getwdFunc to be called")
	}
	if !shouldPromptCalled {
		t.Error("expected shouldPromptGitHubSetup to be called")
	}
}

func TestPromptGitHubMCPSetupIfNeeded_DetectRepoNil(t *testing.T) {
	// Save original functions
	originalGetwd := getwdFunc
	originalShouldPrompt := shouldPromptGitHubSetup
	originalDetectRepo := detectGitHubRepo

	getwdFunc = func() (string, error) {
		return "/test/dir", nil
	}

	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		return true // Should prompt
	}

	detectCalled := false
	detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
		detectCalled = true
		return nil // Not a GitHub repo
	}

	defer func() {
		getwdFunc = originalGetwd
		shouldPromptGitHubSetup = originalShouldPrompt
		detectGitHubRepo = originalDetectRepo
	}()

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt: false,
			MCP:        mcp.MCPConfig{},
		},
	}
	mockAgt := &mockAgent{configManager: mockCfgMgr}

	promptGitHubMCPSetupIfNeeded(mockAgt)

	if !detectCalled {
		t.Error("expected detectGitHubRepo to be called")
	}
}

// =============================================================================
// Test cases for user input handling
// =============================================================================

func TestPromptGitHubSetupVariants_UserInput(t *testing.T) {
	testCases := []struct {
		name           string
		input          string
		setupSucceeds  bool
		saveErr        error
		expectSetup    bool
		expectSave     bool
		expectRefresh  bool
	}{
		{
			name:           "s - setup",
			input:          "s\n",
			setupSucceeds:  true,
			saveErr:        nil,
			expectSetup:    true,
			expectSave:     true,
			expectRefresh:  true,
		},
		{
			name:           "setup - setup",
			input:          "setup\n",
			setupSucceeds:  true,
			saveErr:        nil,
			expectSetup:    true,
			expectSave:     true,
			expectRefresh:  true,
		},
		{
			name:           "yes - setup",
			input:          "yes\n",
			setupSucceeds:  true,
			saveErr:        nil,
			expectSetup:    true,
			expectSave:     true,
			expectRefresh:  true,
		},
		{
			name:           "y - setup",
			input:          "y\n",
			setupSucceeds:  true,
			saveErr:        nil,
			expectSetup:    true,
			expectSave:     true,
			expectRefresh:  true,
		},
		{
			name:           "n - dismiss prompt",
			input:          "n\n",
			setupSucceeds:  false,
			saveErr:        nil,
			expectSetup:    false,
			expectSave:     false,
			expectRefresh:  false,
		},
		{
			name:           "never - dismiss prompt",
			input:          "never\n",
			setupSucceeds:  false,
			saveErr:        nil,
			expectSetup:    false,
			expectSave:     false,
			expectRefresh:  false,
		},
		{
			name:           "no - dismiss prompt",
			input:          "no\n",
			setupSucceeds:  false,
			saveErr:        nil,
			expectSetup:    false,
			expectSave:     false,
			expectRefresh:  false,
		},
		{
			name:           "empty - skip silently",
			input:          "\n",
			setupSucceeds:  false,
			saveErr:        nil,
			expectSetup:    false,
			expectSave:     false,
			expectRefresh:  false,
		},
		{
			name:           "garbage - skip silently",
			input:          "xyz123\n",
			setupSucceeds:  false,
			saveErr:        nil,
			expectSetup:    false,
			expectSave:     false,
			expectRefresh:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Save all original functions
			origGetwd := getwdFunc
			origShouldPrompt := shouldPromptGitHubSetup
			origDetect := detectGitHubRepo
			origRunSetup := runGitHubMCPSetup
			origSave := saveGitHubMCPServer
			origStdin := os.Stdin
			origNewReader := newReaderFromStdin

			// Setup mocks
			getwdFunc = func() (string, error) {
				return "/test/dir", nil
			}

			shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
				return true
			}

			repoInfo := &mcp.GitHubRepoInfo{Owner: "testowner", Repo: "testrepo"}
			detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
				return repoInfo
			}

			setupCalled := false
			runGitHubMCPSetup = func(ctx context.Context, repo *mcp.GitHubRepoInfo, reader *bufio.Reader) (*mcp.MCPServerConfig, error) {
				setupCalled = true
				if tc.setupSucceeds {
					return &mcp.MCPServerConfig{
						Name: "github",
						Type: "http",
						URL:  "https://api.githubcopilot.com/mcp/",
					}, nil
				}
				return nil, errors.New("setup failed")
			}

			saveCalled := false
			saveGitHubMCPServer = func(config *mcp.MCPServerConfig) error {
				saveCalled = true
				return tc.saveErr
			}

			// Create pipe for stdin
			r, w, _ := os.Pipe()
			go func() {
				w.WriteString(tc.input)
				w.Close()
			}()
			os.Stdin = r

			newReaderFromStdin = func(in *os.File) *bufio.Reader {
				return bufio.NewReader(in)
			}

			// Create mock agent - use a different approach that allows override
			updateCalled := false
			testConfig := &configuration.Config{
				SkipPrompt:        false,
				MCP:               mcp.MCPConfig{},
				DismissedPrompts: make(map[string]bool),
			}
			mockCfgMgr := &configManagerWithOverride{
				mockConfigManager: &mockConfigManager{
					config: testConfig,
				},
				updateFn: func(updateFunc func(c *configuration.Config) error) error {
					updateCalled = true
					return updateFunc(testConfig)
				},
			}

			mockAgt := &mockAgent{
				configManager:    mockCfgMgr,
				mcpToolsRefreshed: false,
			}

			// Call the function
			promptGitHubMCPSetupIfNeeded(mockAgt)

			// Verify expectations
			if tc.expectSetup && !setupCalled {
				t.Error("expected runGitHubMCPSetup to be called")
			}
			if tc.expectSave && !saveCalled {
				t.Error("expected saveGitHubMCPServer to be called")
			}
			if tc.expectRefresh && !mockAgt.mcpToolsRefreshed {
				t.Error("expected RefreshMCPTools to be called")
			}

			// For "n" case, check if DismissedPrompts was updated
			if tc.name == "n - dismiss prompt" || tc.name == "never - dismiss prompt" || tc.name == "no - dismiss prompt" {
				if !updateCalled {
					t.Error("expected UpdateConfig to be called to save dismissed prompt")
				}
			}

			// Restore originals
			getwdFunc = origGetwd
			shouldPromptGitHubSetup = origShouldPrompt
			detectGitHubRepo = origDetect
			runGitHubMCPSetup = origRunSetup
			saveGitHubMCPServer = origSave
			os.Stdin = origStdin
			newReaderFromStdin = origNewReader
		})
	}
}

// =============================================================================
// Error handling tests
// =============================================================================

func TestPromptGitHubMCPSetupIfNeeded_SaveServerFails(t *testing.T) {
	origGetwd := getwdFunc
	origShouldPrompt := shouldPromptGitHubSetup
	origDetect := detectGitHubRepo
	origRunSetup := runGitHubMCPSetup
	origSave := saveGitHubMCPServer
	origStdin := os.Stdin
	origNewReader := newReaderFromStdin

	defer func() {
		getwdFunc = origGetwd
		shouldPromptGitHubSetup = origShouldPrompt
		detectGitHubRepo = origDetect
		runGitHubMCPSetup = origRunSetup
		saveGitHubMCPServer = origSave
		os.Stdin = origStdin
		newReaderFromStdin = origNewReader
	}()

	getwdFunc = func() (string, error) {
		return "/test/dir", nil
	}

	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		return true
	}

	repoInfo := &mcp.GitHubRepoInfo{Owner: "testowner", Repo: "testrepo"}
	detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
		return repoInfo
	}

	runGitHubMCPSetup = func(ctx context.Context, repo *mcp.GitHubRepoInfo, reader *bufio.Reader) (*mcp.MCPServerConfig, error) {
		return &mcp.MCPServerConfig{
			Name: "github",
			Type: "http",
			URL:  "https://api.githubcopilot.com/mcp/",
		}, nil
	}

	// Make saveGitHubMCPServer fail
	saveGitHubMCPServer = func(config *mcp.MCPServerConfig) error {
		return errors.New("save failed")
	}

	// Create pipe for stdin
	r, w, _ := os.Pipe()
	go func() {
		w.WriteString("s\n")
		w.Close()
	}()
	os.Stdin = r

	newReaderFromStdin = func(in *os.File) *bufio.Reader {
		return bufio.NewReader(in)
	}

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt:        false,
			MCP:               mcp.MCPConfig{},
			DismissedPrompts: make(map[string]bool),
		},
	}

	mockAgt := &mockAgent{
		configManager:    mockCfgMgr,
		mcpToolsRefreshed: false,
	}

	// This should not panic - just log a warning
	promptGitHubMCPSetupIfNeeded(mockAgt)

	// Refresh should NOT be called since save failed
	if mockAgt.mcpToolsRefreshed {
		t.Error("expected RefreshMCPTools NOT to be called when save fails")
	}
}

func TestPromptGitHubMCPSetupIfNeeded_SetupFails(t *testing.T) {
	origGetwd := getwdFunc
	origShouldPrompt := shouldPromptGitHubSetup
	origDetect := detectGitHubRepo
	origRunSetup := runGitHubMCPSetup
	origStdin := os.Stdin
	origNewReader := newReaderFromStdin

	defer func() {
		getwdFunc = origGetwd
		shouldPromptGitHubSetup = origShouldPrompt
		detectGitHubRepo = origDetect
		runGitHubMCPSetup = origRunSetup
		os.Stdin = origStdin
		newReaderFromStdin = origNewReader
	}()

	getwdFunc = func() (string, error) {
		return "/test/dir", nil
	}

	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		return true
	}

	repoInfo := &mcp.GitHubRepoInfo{Owner: "testowner", Repo: "testrepo"}
	detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
		return repoInfo
	}

	// Make runGitHubMCPSetup fail
	runGitHubMCPSetup = func(ctx context.Context, repo *mcp.GitHubRepoInfo, reader *bufio.Reader) (*mcp.MCPServerConfig, error) {
		return nil, errors.New("setup failed")
	}

	// Create pipe for stdin
	r, w, _ := os.Pipe()
	go func() {
		w.WriteString("s\n")
		w.Close()
	}()
	os.Stdin = r

	newReaderFromStdin = func(in *os.File) *bufio.Reader {
		return bufio.NewReader(in)
	}

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt:        false,
			MCP:               mcp.MCPConfig{},
			DismissedPrompts: make(map[string]bool),
		},
	}

	mockAgt := &mockAgent{
		configManager:    mockCfgMgr,
		mcpToolsRefreshed: false,
	}

	// This should not panic - just return
	promptGitHubMCPSetupIfNeeded(mockAgt)
}

func TestPromptGitHubMCPSetupIfNeeded_UpdateConfigFails(t *testing.T) {
	origGetwd := getwdFunc
	origShouldPrompt := shouldPromptGitHubSetup
	origDetect := detectGitHubRepo
	origStdin := os.Stdin
	origNewReader := newReaderFromStdin

	defer func() {
		getwdFunc = origGetwd
		shouldPromptGitHubSetup = origShouldPrompt
		detectGitHubRepo = origDetect
		os.Stdin = origStdin
		newReaderFromStdin = origNewReader
	}()

	getwdFunc = func() (string, error) {
		return "/test/dir", nil
	}

	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		return true
	}

	repoInfo := &mcp.GitHubRepoInfo{Owner: "testowner", Repo: "testrepo"}
	detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
		return repoInfo
	}

	// Create pipe for stdin
	r, w, _ := os.Pipe()
	go func() {
		w.WriteString("n\n")
		w.Close()
	}()
	os.Stdin = r

	newReaderFromStdin = func(in *os.File) *bufio.Reader {
		return bufio.NewReader(in)
	}

	mockCfgMgr := &configManagerWithOverride{
		mockConfigManager: &mockConfigManager{
			config: &configuration.Config{
				SkipPrompt:        false,
				MCP:               mcp.MCPConfig{},
				DismissedPrompts: make(map[string]bool),
			},
		},
		updateFn: func(updateFunc func(c *configuration.Config) error) error {
			return errors.New("update config failed")
		},
	}

	mockAgt := &mockAgent{
		configManager:    mockCfgMgr,
		mcpToolsRefreshed: false,
	}

	// This should not panic
	promptGitHubMCPSetupIfNeeded(mockAgt)
}

func TestPromptGitHubMCPSetupIfNeeded_RefreshToolsFails(t *testing.T) {
	origGetwd := getwdFunc
	origShouldPrompt := shouldPromptGitHubSetup
	origDetect := detectGitHubRepo
	origRunSetup := runGitHubMCPSetup
	origSave := saveGitHubMCPServer
	origStdin := os.Stdin
	origNewReader := newReaderFromStdin

	defer func() {
		getwdFunc = origGetwd
		shouldPromptGitHubSetup = origShouldPrompt
		detectGitHubRepo = origDetect
		runGitHubMCPSetup = origRunSetup
		saveGitHubMCPServer = origSave
		os.Stdin = origStdin
		newReaderFromStdin = origNewReader
	}()

	getwdFunc = func() (string, error) {
		return "/test/dir", nil
	}

	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		return true
	}

	repoInfo := &mcp.GitHubRepoInfo{Owner: "testowner", Repo: "testrepo"}
	detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
		return repoInfo
	}

	runGitHubMCPSetup = func(ctx context.Context, repo *mcp.GitHubRepoInfo, reader *bufio.Reader) (*mcp.MCPServerConfig, error) {
		return &mcp.MCPServerConfig{
			Name: "github",
			Type: "http",
			URL:  "https://api.githubcopilot.com/mcp/",
		}, nil
	}

	saveGitHubMCPServer = func(config *mcp.MCPServerConfig) error {
		return nil
	}

	// Create pipe for stdin
	r, w, _ := os.Pipe()
	go func() {
		w.WriteString("s\n")
		w.Close()
	}()
	os.Stdin = r

	newReaderFromStdin = func(in *os.File) *bufio.Reader {
		return bufio.NewReader(in)
	}

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt:        false,
			MCP:               mcp.MCPConfig{},
			DismissedPrompts: make(map[string]bool),
		},
	}

	mockAgt := &mockAgent{
		configManager: mockCfgMgr,
		refreshErr:    errors.New("refresh failed"),
	}

	// This should not panic even if RefreshMCPTools fails
	promptGitHubMCPSetupIfNeeded(mockAgt)
}

func TestPromptGitHubMCPSetupIfNeeded_SetupCancelled(t *testing.T) {
	origGetwd := getwdFunc
	origShouldPrompt := shouldPromptGitHubSetup
	origDetect := detectGitHubRepo
	origRunSetup := runGitHubMCPSetup
	origSave := saveGitHubMCPServer
	origStdin := os.Stdin
	origNewReader := newReaderFromStdin

	defer func() {
		getwdFunc = origGetwd
		shouldPromptGitHubSetup = origShouldPrompt
		detectGitHubRepo = origDetect
		runGitHubMCPSetup = origRunSetup
		saveGitHubMCPServer = origSave
		os.Stdin = origStdin
		newReaderFromStdin = origNewReader
	}()

	getwdFunc = func() (string, error) {
		return "/test/dir", nil
	}

	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		return true
	}

	repoInfo := &mcp.GitHubRepoInfo{Owner: "testowner", Repo: "testrepo"}
	detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
		return repoInfo
	}

	// Make runGitHubMCPSetup return nil server (cancelled)
	runGitHubMCPSetup = func(ctx context.Context, repo *mcp.GitHubRepoInfo, reader *bufio.Reader) (*mcp.MCPServerConfig, error) {
		return nil, nil
	}

	saveCalled := false
	saveGitHubMCPServer = func(config *mcp.MCPServerConfig) error {
		saveCalled = true
		return nil
	}

	// Create pipe for stdin
	r, w, _ := os.Pipe()
	go func() {
		w.WriteString("s\n")
		w.Close()
	}()
	os.Stdin = r

	newReaderFromStdin = func(in *os.File) *bufio.Reader {
		return bufio.NewReader(in)
	}

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt:        false,
			MCP:               mcp.MCPConfig{},
			DismissedPrompts: make(map[string]bool),
		},
	}

	mockAgt := &mockAgent{
		configManager: mockCfgMgr,
	}

	promptGitHubMCPSetupIfNeeded(mockAgt)

	// Save should not be called when server is nil
	if saveCalled {
		t.Error("expected saveGitHubMCPServer NOT to be called when setup returns nil server")
	}
}

// =============================================================================
// Edge case tests
// =============================================================================

func TestPromptGitHubMCPSetupIfNeeded_NilDismissedPrompts(t *testing.T) {
	origGetwd := getwdFunc
	origShouldPrompt := shouldPromptGitHubSetup

	defer func() {
		getwdFunc = origGetwd
		shouldPromptGitHubSetup = origShouldPrompt
	}()

	getwdFunc = func() (string, error) {
		return "/test/dir", nil
	}

	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		// Should not panic with nil DismissedPrompts
		if dismissedPrompts == nil {
			t.Log("dismissedPrompts is nil - this is expected")
		}
		return false // Don't prompt
	}

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt:        false,
			MCP:               mcp.MCPConfig{},
			DismissedPrompts: nil, // nil map
		},
	}

	mockAgt := &mockAgent{
		configManager: mockCfgMgr,
	}

	// Should not panic
	promptGitHubMCPSetupIfNeeded(mockAgt)
}

func TestPromptGitHubMCPSetupIfNeeded_NilMCPConfig(t *testing.T) {
	// Test with nil MCP config (should not panic)
	origGetwd := getwdFunc
	origShouldPrompt := shouldPromptGitHubSetup

	defer func() {
		getwdFunc = origGetwd
		shouldPromptGitHubSetup = origShouldPrompt
	}()

	getwdFunc = func() (string, error) {
		return "/test/dir", nil
	}

	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		// Access cfg.Servers to ensure we handle nil properly
		if cfg.Servers == nil {
			t.Log("MCP Servers is nil - handled correctly")
		}
		return false
	}

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt: false,
			MCP:        mcp.MCPConfig{}, // Empty but not nil
		},
	}

	mockAgt := &mockAgent{
		configManager: mockCfgMgr,
	}

	promptGitHubMCPSetupIfNeeded(mockAgt)
}

func TestPromptGitHubMCPSetupIfNeeded_ReaderError(t *testing.T) {
	// Test that reader errors are handled gracefully
	origGetwd := getwdFunc
	origShouldPrompt := shouldPromptGitHubSetup
	origDetect := detectGitHubRepo
	origStdin := os.Stdin
	origNewReader := newReaderFromStdin

	defer func() {
		getwdFunc = origGetwd
		shouldPromptGitHubSetup = origShouldPrompt
		detectGitHubRepo = origDetect
		os.Stdin = origStdin
		newReaderFromStdin = origNewReader
	}()

	getwdFunc = func() (string, error) {
		return "/test/dir", nil
	}

	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		return true
	}

	repoInfo := &mcp.GitHubRepoInfo{Owner: "testowner", Repo: "testrepo"}
	detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
		return repoInfo
	}

	// Create a pipe that closes immediately to cause a read error
	r, w, _ := os.Pipe()
	w.Close() // Close write end immediately so read returns EOF
	os.Stdin = r

	newReaderFromStdin = func(in *os.File) *bufio.Reader {
		return bufio.NewReader(in)
	}

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt:        false,
			MCP:               mcp.MCPConfig{},
			DismissedPrompts: make(map[string]bool),
		},
	}

	mockAgt := &mockAgent{
		configManager: mockCfgMgr,
	}

	// This should not panic - just return on read error
	promptGitHubMCPSetupIfNeeded(mockAgt)
}

// =============================================================================
// Tests for dismiss prompt updates
// =============================================================================

func TestPromptGitHubMCPSetupIfNeeded_DismissedPromptUpdated(t *testing.T) {
	// Test that DismissedPrompts is properly updated when user chooses "n"
	origGetwd := getwdFunc
	origShouldPrompt := shouldPromptGitHubSetup
	origDetect := detectGitHubRepo
	origStdin := os.Stdin
	origNewReader := newReaderFromStdin

	defer func() {
		getwdFunc = origGetwd
		shouldPromptGitHubSetup = origShouldPrompt
		detectGitHubRepo = origDetect
		os.Stdin = origStdin
		newReaderFromStdin = origNewReader
	}()

	getwdFunc = func() (string, error) {
		return "/test/dir", nil
	}

	shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
		return true
	}

	repoInfo := &mcp.GitHubRepoInfo{Owner: "testowner", Repo: "testrepo"}
	detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
		return repoInfo
	}

	// Create pipe for stdin with "n\n"
	r, w, _ := os.Pipe()
	go func() {
		w.WriteString("n\n")
		w.Close()
	}()
	os.Stdin = r

	newReaderFromStdin = func(in *os.File) *bufio.Reader {
		return bufio.NewReader(in)
	}

	mockCfgMgr := &mockConfigManager{
		config: &configuration.Config{
			SkipPrompt:        false,
			MCP:               mcp.MCPConfig{},
			DismissedPrompts: make(map[string]bool),
		},
	}

	mockAgt := &mockAgent{
		configManager: mockCfgMgr,
	}

	promptGitHubMCPSetupIfNeeded(mockAgt)

	// Verify DismissedPrompts was updated
	if mockCfgMgr.config.DismissedPrompts == nil {
		t.Error("expected DismissedPrompts to be initialized")
	}
	if !mockCfgMgr.config.DismissedPrompts["github_mcp_setup"] {
		t.Error("expected github_mcp_setup to be true in DismissedPrompts")
	}
}

// Test that multiple "yes" variants all trigger setup
func TestPromptGitHubMCPSetupIfNeeded_YesVariants(t *testing.T) {
	variants := []string{"s", "setup", "yes", "y"}

	for _, variant := range variants {
		t.Run("variant_"+variant, func(t *testing.T) {
			origGetwd := getwdFunc
			origShouldPrompt := shouldPromptGitHubSetup
			origDetect := detectGitHubRepo
			origRunSetup := runGitHubMCPSetup
			origSave := saveGitHubMCPServer
			origStdin := os.Stdin
			origNewReader := newReaderFromStdin

			defer func() {
				getwdFunc = origGetwd
				shouldPromptGitHubSetup = origShouldPrompt
				detectGitHubRepo = origDetect
				runGitHubMCPSetup = origRunSetup
				saveGitHubMCPServer = origSave
				os.Stdin = origStdin
				newReaderFromStdin = origNewReader
			}()

			getwdFunc = func() (string, error) {
				return "/test/dir", nil
			}

			shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
				return true
			}

			repoInfo := &mcp.GitHubRepoInfo{Owner: "testowner", Repo: "testrepo"}
			detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
				return repoInfo
			}

			setupCalled := false
			runGitHubMCPSetup = func(ctx context.Context, repo *mcp.GitHubRepoInfo, reader *bufio.Reader) (*mcp.MCPServerConfig, error) {
				setupCalled = true
				return &mcp.MCPServerConfig{Name: "github"}, nil
			}

			saveGitHubMCPServer = func(config *mcp.MCPServerConfig) error {
				return nil
			}

			// Create pipe with variant input
			r, w, _ := os.Pipe()
			go func() {
				w.WriteString(variant + "\n")
				w.Close()
			}()
			os.Stdin = r

			newReaderFromStdin = func(in *os.File) *bufio.Reader {
				return bufio.NewReader(in)
			}

			mockCfgMgr := &mockConfigManager{
				config: &configuration.Config{
					SkipPrompt:        false,
					MCP:               mcp.MCPConfig{},
					DismissedPrompts: make(map[string]bool),
				},
			}

			mockAgt := &mockAgent{
				configManager:    mockCfgMgr,
				mcpToolsRefreshed: false,
			}

			promptGitHubMCPSetupIfNeeded(mockAgt)

			if !setupCalled {
				t.Errorf("expected runGitHubMCPSetup to be called for variant %q", variant)
			}
		})
	}
}

// Test that multiple "no" variants all trigger dismiss
func TestPromptGitHubMCPSetupIfNeeded_NoVariants(t *testing.T) {
	variants := []string{"n", "never", "no"}

	for _, variant := range variants {
		t.Run("variant_"+variant, func(t *testing.T) {
			origGetwd := getwdFunc
			origShouldPrompt := shouldPromptGitHubSetup
			origDetect := detectGitHubRepo
			origStdin := os.Stdin
			origNewReader := newReaderFromStdin

			defer func() {
				getwdFunc = origGetwd
				shouldPromptGitHubSetup = origShouldPrompt
				detectGitHubRepo = origDetect
				os.Stdin = origStdin
				newReaderFromStdin = origNewReader
			}()

			getwdFunc = func() (string, error) {
				return "/test/dir", nil
			}

			shouldPromptGitHubSetup = func(workingDir string, cfg mcp.MCPConfig, dismissedPrompts map[string]bool) bool {
				return true
			}

			repoInfo := &mcp.GitHubRepoInfo{Owner: "testowner", Repo: "testrepo"}
			detectGitHubRepo = func(workingDir string) *mcp.GitHubRepoInfo {
				return repoInfo
			}

			// Create pipe with variant input
			r, w, _ := os.Pipe()
			go func() {
				w.WriteString(variant + "\n")
				w.Close()
			}()
			os.Stdin = r

			newReaderFromStdin = func(in *os.File) *bufio.Reader {
				return bufio.NewReader(in)
			}

			mockCfgMgr := &mockConfigManager{
				config: &configuration.Config{
					SkipPrompt:        false,
					MCP:               mcp.MCPConfig{},
					DismissedPrompts: make(map[string]bool),
				},
			}

			mockAgt := &mockAgent{
				configManager: mockCfgMgr,
			}

			promptGitHubMCPSetupIfNeeded(mockAgt)

			// Verify DismissedPrompts was updated
			if !mockCfgMgr.config.DismissedPrompts["github_mcp_setup"] {
				t.Errorf("expected github_mcp_setup to be true for variant %q", variant)
			}
		})
	}
}