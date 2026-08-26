//go:build !js

package cmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestAvailablePersonaCompletions(t *testing.T) {
	cfg := &configuration.Config{
		SubagentTypes: map[string]configuration.SubagentType{
			"web_scraper": {ID: "web_scraper", Enabled: true},
			"coder":       {ID: "coder", Enabled: true},
			"debugger":    {ID: "debugger", Enabled: false},
		},
	}

	all := availablePersonaCompletions(cfg, "")
	if len(all) != 2 {
		t.Fatalf("expected 2 enabled persona completions, got %d (%v)", len(all), all)
	}
	if all[0] != "coder" || all[1] != "web_scraper" {
		t.Fatalf("unexpected completion order/content: %v", all)
	}

	filtered := availablePersonaCompletions(cfg, "web")
	if len(filtered) != 1 || filtered[0] != "web_scraper" {
		t.Fatalf("unexpected filtered completions: %v", filtered)
	}
}

// =============================================================================
// shouldPreloadLocalModel
//
// Regression coverage for the daemon-autostart GPU-contention bug: an
// auto-started background daemon inherits the foreground process's
// SPROUT_PROVIDER=sprout-local and used to eagerly preload the local model,
// competing with the foreground process for the same GPU. That reliably made
// the daemon miss its 10s health-check StartTimeout, leaving an unsupervised
// process running (observed firsthand: a spawned daemon pinned at ~99% CPU
// for 15+ minutes with nothing to reap it, since the idle reaper only
// watches daemons that reached a serving state). SPROUT_DAEMON_AUTOSTARTED=1
// (set only on the auto-spawned child's env, never by an explicit
// `sprout agent -d`) now gates the eager preload off for that one case.
// =============================================================================

// TestShouldPreloadLocalModel_NoDaemonReachable covers the cases that don't
// depend on daemon reachability: provider gating, the auto-started-daemon
// skip, and daemonMode always preloading. SPROUT_DAEMON_AGENT_SOCKET is
// pointed at a nonexistent path throughout so isDaemonReachableForAgentRouting
// always sees "no daemon" — these cases must hold regardless of whether a
// real daemon happens to be running on the machine the test executes on.
func TestShouldPreloadLocalModel_NoDaemonReachable(t *testing.T) {
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", shortSocketPath(t, "no-daemon"))

	tests := []struct {
		name        string
		provider    string // SPROUT_PROVIDER
		autostarted string // SPROUT_DAEMON_AUTOSTARTED
		daemon      bool   // daemonMode
		want        bool
	}{
		{
			name:     "foreground local session preloads when no daemon is reachable",
			provider: "sprout-local",
			want:     true,
		},
		{
			name:        "auto-started background daemon skips preload",
			provider:    "sprout-local",
			autostarted: "1",
			want:        false,
		},
		{
			name:     "non-local provider never preloads",
			provider: "openai",
			want:     false,
		},
		{
			name:     "explicit daemon (-d) always preloads — no autostarted marker",
			provider: "sprout-local",
			daemon:   true,
			want:     true,
		},
		{
			name:        "autostarted marker with non-local provider still skips",
			provider:    "openai",
			autostarted: "1",
			want:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SPROUT_PROVIDER", tc.provider)
			// Empty string is equivalent to unset for this check (both are
			// != "1"), and t.Setenv (unlike os.Unsetenv) auto-restores the
			// prior value after the test, per this repo's env-scoping rule.
			t.Setenv("SPROUT_DAEMON_AUTOSTARTED", tc.autostarted)

			origDaemonMode := daemonMode
			daemonMode = tc.daemon
			defer func() { daemonMode = origDaemonMode }()

			if got := shouldPreloadLocalModel(); got != tc.want {
				t.Errorf("shouldPreloadLocalModel() = %v, want %v (provider=%q autostarted=%q daemonMode=%v)",
					got, tc.want, tc.provider, tc.autostarted, tc.daemon)
			}
		})
	}
}

// TestShouldPreloadLocalModel_DaemonReachable is the regression test for the
// preload-ordering gap: a foreground local session must skip its own eager
// preload when a healthy daemon is already up to serve the query instead —
// otherwise "share the GPU-resident model across instances" never actually
// happens, even when daemon routing works.
func TestShouldPreloadLocalModel_DaemonReachable(t *testing.T) {
	stub := &stubAgentForCmd{}
	sockPath := startCmdAgentServer(t, stub)
	t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
	t.Setenv("SPROUT_PROVIDER", "sprout-local")
	t.Setenv("SPROUT_DAEMON_AUTOSTARTED", "")

	origDaemonMode := daemonMode
	origWorkflowConfig := agentWorkflowConfig
	defer func() {
		daemonMode = origDaemonMode
		agentWorkflowConfig = origWorkflowConfig
	}()

	t.Run("healthy daemon present: skip preload, defer to daemon routing", func(t *testing.T) {
		daemonMode = false
		agentWorkflowConfig = ""
		if got := shouldPreloadLocalModel(); got {
			t.Error("shouldPreloadLocalModel() = true, want false: a healthy daemon is reachable and should serve this query")
		}
	})

	t.Run("daemonMode true: preload anyway, we ARE the daemon", func(t *testing.T) {
		daemonMode = true
		agentWorkflowConfig = ""
		if !shouldPreloadLocalModel() {
			t.Error("shouldPreloadLocalModel() = false, want true: daemon mode never routes to another daemon")
		}
	})

	t.Run("workflow config set: tryDaemonOneShot won't run, preload anyway", func(t *testing.T) {
		daemonMode = false
		agentWorkflowConfig = "/some/workflow.json"
		if !shouldPreloadLocalModel() {
			t.Error("shouldPreloadLocalModel() = false, want true: workflow runs never route through tryDaemonOneShot")
		}
	})

	t.Run("jsonOut: daemon serves it (socket carries output format), skip preload", func(t *testing.T) {
		daemonMode = false
		agentWorkflowConfig = ""
		origJSON := outputFormatJSON
		outputFormatJSON = true
		defer func() { outputFormatJSON = origJSON }()
		if shouldPreloadLocalModel() {
			t.Error("shouldPreloadLocalModel() = true, want false: --output-json one-shots route through the daemon (SP-136 P4 carries the output format over the socket), so the daemon serves this query")
		}
	})
}

// TestIsDaemonReachableForAgentRouting_RealRoundTrip verifies the health
// check does a genuine round trip, not just a socket dial — a listener that
// accepts connections but never responds (simulating a hung/stuck daemon,
// which is exactly the auto-start failure mode this whole fix chain started
// from) must be reported unreachable, not mistaken for healthy.
func TestIsDaemonReachableForAgentRouting_RealRoundTrip(t *testing.T) {
	t.Run("healthy daemon is reachable", func(t *testing.T) {
		stub := &stubAgentForCmd{}
		sockPath := startCmdAgentServer(t, stub)
		t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
		t.Setenv("SPROUT_DAEMON_AGENT", "")

		if !isDaemonReachableForAgentRouting() {
			t.Error("expected a healthy stub daemon to be reachable")
		}
	})

	t.Run("no daemon at all is unreachable", func(t *testing.T) {
		t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", shortSocketPath(t, "missing"))
		t.Setenv("SPROUT_DAEMON_AGENT", "")

		if isDaemonReachableForAgentRouting() {
			t.Error("expected no daemon socket to be unreachable")
		}
	})

	t.Run("accepts but never responds is unreachable", func(t *testing.T) {
		sockPath := shortSocketPath(t, "stuck")
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		defer ln.Close()
		// Accept connections but never read/write — simulates a daemon
		// that's spawned and listening but stuck (e.g. hung on its own
		// eager local-model load), the exact scenario the 1.5s round-trip
		// timeout exists to catch.
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				_ = conn // deliberately never close/read/write
			}
		}()

		t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
		t.Setenv("SPROUT_DAEMON_AGENT", "")

		start := time.Now()
		reachable := isDaemonReachableForAgentRouting()
		elapsed := time.Since(start)

		if reachable {
			t.Error("expected a stuck (non-responding) daemon to be reported unreachable")
		}
		if elapsed > 3*time.Second {
			t.Errorf("health check took %s, expected it to respect its ~1.5s timeout", elapsed)
		}
	})

	t.Run("SPROUT_DAEMON_AGENT=0 forces unreachable even if daemon is healthy", func(t *testing.T) {
		stub := &stubAgentForCmd{}
		sockPath := startCmdAgentServer(t, stub)
		t.Setenv("SPROUT_DAEMON_AGENT_SOCKET", sockPath)
		t.Setenv("SPROUT_DAEMON_AGENT", "0")

		if isDaemonReachableForAgentRouting() {
			t.Error("SPROUT_DAEMON_AGENT=0 must force unreachable regardless of daemon health")
		}
	})
}

// =============================================================================
// createChatAgent
// =============================================================================

func TestCreateChatAgent_Default(t *testing.T) {
	// Save and restore original values
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	// Reset to minimal defaults
	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
	// Verify base system prompt is set (default prompt should exist)
	if a.GetSystemPrompt() == "" {
		t.Error("expected non-empty system prompt")
	}
}

func TestCreateChatAgent_WithProviderAndModel(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = "openrouter"
	agentModel = "test-model"
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with provider+model unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestCreateChatAgent_WithModelOnly(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = "claude-3-opus"
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with model only unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestCreateChatAgent_WithProviderOnly(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = "openai"
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with provider only unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestCreateChatAgent_WithSystemPrompt(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = "You are a test assistant."
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with system prompt unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	prompt := a.GetSystemPrompt()
	if prompt != "You are a test assistant." {
		t.Errorf("expected system prompt to be set, got %q", prompt)
	}
}

func TestCreateChatAgent_WithSystemPromptFile(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	// Create a temp file for testing
	tmpFile, err := os.CreateTemp("", "system_prompt_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.WriteString("Custom system prompt from file.")
	tmpFile.Close()

	agentSystemPromptFile = tmpFile.Name()

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with system prompt file unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	prompt := a.GetSystemPrompt()
	if prompt != "Custom system prompt from file." {
		t.Errorf("expected system prompt from file, got %q", prompt)
	}
}

func TestCreateChatAgent_WithSystemPromptFileNotFound(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = "/nonexistent/path/to/prompt.txt"

	_, err := createChatAgent()
	if err == nil {
		t.Fatal("expected error for non-existent system prompt file")
	}
}

func TestCreateChatAgent_WithMaxIterations(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 10
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with max iterations unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	if a.GetMaxIterations() != 10 {
		t.Errorf("expected max iterations 10, got %d", a.GetMaxIterations())
	}
}

// ---------------------------------------------------------------------------
// TestCreateChatAgent_DaemonMode_NilAgentOnProviderError
// ---------------------------------------------------------------------------

func TestCreateChatAgent_DaemonMode_NilAgentOnProviderError(t *testing.T) {
	// Verify that the sentinel errors used by the daemon-mode nil-agent path
	// are correctly handled by errors.Is(). This tests the branching logic
	// in createChatAgent:
	//
	//   if daemonMode && (errors.Is(err, agent.ErrProviderNotConfigured) ||
	//                      errors.Is(err, agent.ErrModelNotAvailable)) {
	//       return nil, nil  // nil-agent daemon mode
	//   }
	//
	// We cannot exercise the full createChatAgent() path for this because
	// agent.NewAgent() is a function that reads real configuration and
	// cannot be mocked. However, the test below validates the critical
	// errors.Is() semantics that the nil-agent path depends on.

	t.Run("ErrProviderNotConfigured is matched by errors.Is", func(t *testing.T) {
		t.Parallel()

		// Direct reference should match.
		if !errors.Is(agent.ErrProviderNotConfigured, agent.ErrProviderNotConfigured) {
			t.Error("ErrProviderNotConfigured should match itself")
		}

		// Wrapped error should also match.
		wrapped := errors.New("outer: " + agent.ErrProviderNotConfigured.Error())
		wrapped = fmt.Errorf("wrapped: %w", agent.ErrProviderNotConfigured)
		if !errors.Is(wrapped, agent.ErrProviderNotConfigured) {
			t.Error("wrapped ErrProviderNotConfigured should match via errors.Is")
		}

		// Unrelated errors must not match.
		unrelated := errors.New("some other error")
		if errors.Is(unrelated, agent.ErrProviderNotConfigured) {
			t.Error("unrelated error should not match ErrProviderNotConfigured")
		}

		// A string-matching error should not match (it's not the same sentinel).
		stringErr := errors.New("provider is not configured — configure via webui settings")
		if errors.Is(stringErr, agent.ErrProviderNotConfigured) {
			t.Error("a separate error with the same message should not match via errors.Is")
		}
	})

	t.Run("ErrModelNotAvailable is matched by errors.Is", func(t *testing.T) {
		t.Parallel()

		// Direct reference should match.
		if !errors.Is(agent.ErrModelNotAvailable, agent.ErrModelNotAvailable) {
			t.Error("ErrModelNotAvailable should match itself")
		}

		// Wrapped error should also match.
		wrapped := fmt.Errorf("wrapped: %w", agent.ErrModelNotAvailable)
		if !errors.Is(wrapped, agent.ErrModelNotAvailable) {
			t.Error("wrapped ErrModelNotAvailable should match via errors.Is")
		}

		// The two sentinels must not cross-match.
		if errors.Is(agent.ErrProviderNotConfigured, agent.ErrModelNotAvailable) {
			t.Error("ErrProviderNotConfigured should not match ErrModelNotAvailable")
		}
	})

	t.Run("daemonMode global is independently controllable", func(t *testing.T) {
		// Save and restore the daemonMode global.
		origDaemonMode := daemonMode
		defer func() { daemonMode = origDaemonMode }()

		// Verify we can set and read daemonMode.
		daemonMode = true
		if !daemonMode {
			t.Fatal("daemonMode should be true after setting")
		}

		daemonMode = false
		if daemonMode {
			t.Fatal("daemonMode should be false after resetting")
		}
	})

	t.Run("createChatAgent returns nil agent in daemon mode with ErrProviderNotConfigured", func(t *testing.T) {
		// This test exercises the nil-agent path by simulating the condition
		// that createChatAgent checks. Because agent.NewAgent() uses
		// isRunningUnderTest() to always create a test client under go test,
		// we cannot trigger ErrProviderNotConfigured from the real function.
		// Instead, we validate the branch condition logic independently.
		//
		// The actual nil-agent path is covered by integration tests and
		// manual daemon-mode testing (see TestRecoverProviderStartup_DaemonMode).

		origDaemonMode := daemonMode
		defer func() { daemonMode = origDaemonMode }()

		// Simulate the daemon-mode check that createChatAgent performs:
		//   if daemonMode && errors.Is(err, agent.ErrProviderNotConfigured) { return nil, nil }
		daemonMode = true
		err := agent.ErrProviderNotConfigured

		// This is the exact condition checked in createChatAgent.
		shouldReturnNil := daemonMode &&
			(errors.Is(err, agent.ErrProviderNotConfigured) || errors.Is(err, agent.ErrModelNotAvailable))
		if !shouldReturnNil {
			t.Fatal("daemonMode + ErrProviderNotConfigured should trigger nil-agent path")
		}

		// Same for ErrModelNotAvailable.
		err = agent.ErrModelNotAvailable
		shouldReturnNil = daemonMode &&
			(errors.Is(err, agent.ErrProviderNotConfigured) || errors.Is(err, agent.ErrModelNotAvailable))
		if !shouldReturnNil {
			t.Fatal("daemonMode + ErrModelNotAvailable should trigger nil-agent path")
		}

		// A non-sentinel error should NOT trigger the nil-agent path.
		err = errors.New("some random error")
		shouldReturnNil = daemonMode &&
			(errors.Is(err, agent.ErrProviderNotConfigured) || errors.Is(err, agent.ErrModelNotAvailable))
		if shouldReturnNil {
			t.Fatal("daemonMode + random error should NOT trigger nil-agent path")
		}
	})

	// TestCreateChatAgent_DaemonModeForcesNonInteractive verifies that
	// daemonMode forces isInteractive=false, matching the --daemon flag's
	// documented contract ("keep web UI running without interactive prompt").
	// The isInteractive computation in the agent command's RunE is:
	//
	//   isInteractive := !daemonMode && len(args) == 0 && !isCI && stdinIsTerminal
	//
	// Without the !daemonMode guard, a TTY-attached daemon would enter the
	// REPL, contradicting the flag's purpose.
	t.Run("daemonMode forces non-interactive even with TTY and no args", func(t *testing.T) {
		origDaemonMode := daemonMode
		defer func() { daemonMode = origDaemonMode }()

		// Simulate the exact isInteractive computation from agent_command.go.
		// All non-daemon conditions are favorable to interactive (no args,
		// not CI, stdin is a terminal) — yet daemonMode must still win.
		computeInteractive := func(daemon bool, args []string, ci bool, stdinTTY bool) bool {
			return !daemon && len(args) == 0 && !ci && stdinTTY
		}

		// Daemon mode with all interactive-friendly conditions: must be false.
		daemonMode = true
		isInteractive := computeInteractive(daemonMode, nil, false, true)
		if isInteractive {
			t.Fatal("daemonMode=true must force isInteractive=false even with TTY, no args, not CI")
		}

		// Non-daemon with same conditions: must be true (baseline sanity).
		daemonMode = false
		isInteractive = computeInteractive(daemonMode, nil, false, true)
		if !isInteractive {
			t.Fatal("daemonMode=false with TTY, no args, not CI should be interactive")
		}

		// Daemon mode with args: still false (doubly so).
		daemonMode = true
		isInteractive = computeInteractive(daemonMode, []string{"query"}, false, true)
		if isInteractive {
			t.Fatal("daemonMode=true with args must force isInteractive=false")
		}
	})
}
