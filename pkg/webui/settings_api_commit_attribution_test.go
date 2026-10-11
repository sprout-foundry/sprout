//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingsAPI_CommitAttribution(t *testing.T) {
	out := sanitizedConfig(&configuration.Config{CommitAttribution: "trailer"})
	assert.Equal(t, "trailer", out["commit_attribution"])

	for _, tc := range []struct {
		in, want string
		wantErr  bool
	}{
		{"trailer", "trailer", false},
		{"OFF", "off", false},
		{"", "", false},
		{"co-author", "", true},
	} {
		cfg := &configuration.Config{}
		unknown, err := applyPartialSettings(cfg, map[string]interface{}{"commit_attribution": tc.in})
		if tc.wantErr {
			assert.Error(t, err, tc.in)
			continue
		}
		require.NoError(t, err, tc.in)
		assert.Empty(t, unknown)
		assert.Equal(t, tc.want, cfg.CommitAttribution)
	}
}

// TestHandleAPIGitCommit_Attribution checks the web UI commit endpoint
// appends the session agent's Assisted-by trailer only when the setting is
// on.
func TestHandleAPIGitCommit_Attribution(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, on := range []bool{false, true} {
		t.Setenv("SPROUT_CONFIG", t.TempDir())
		dir := t.TempDir()
		runGit := func(args ...string) string {
			cmd := exec.Command("git", args...) //nolint:gosec // G204: test-driven git invocations with controlled args
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "git %v: %s", args, out)
			return strings.TrimSpace(string(out))
		}
		runGit("init", "-q")
		runGit("config", "user.email", "test@example.com")
		runGit("config", "user.name", "Test")
		runGit("config", "commit.gpgsign", "false")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644))
		runGit("add", "a.txt")

		ag, err := agent.NewAgentWithModel("test:test")
		require.NoError(t, err)
		t.Cleanup(ag.Shutdown)
		require.NoError(t, ag.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
			if on {
				cfg.CommitAttribution = configuration.CommitAttributionTrailer
			} else {
				cfg.CommitAttribution = ""
			}
			return nil
		}))

		ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
		require.NoError(t, err)
		ws.mutex.Lock()
		ctx := ws.getOrCreateClientContextLocked(defaultWebClientID)
		ctx.WorkspaceRoot = dir
		ctx.Agent = ag
		ws.mutex.Unlock()

		req := httptest.NewRequest(http.MethodPost, "/api/git/commit", strings.NewReader(`{"message":"fix: add a"}`))
		rec := httptest.NewRecorder()
		ws.handleAPIGitCommit(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		body := runGit("log", "-1", "--format=%B")
		if on {
			assert.Equal(t, "fix: add a\n\n"+git.SproutAssistant(ag.GetProvider(), ag.GetModel()).Trailer(), body)
		} else {
			assert.Equal(t, "fix: add a", body)
		}
	}
}
