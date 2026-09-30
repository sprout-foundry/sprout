//go:build !js

package cmd

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/pythonruntime"
)

var startupChecksOnce sync.Once
var isolatedConfig bool
var debugPprofAddr string
var whyFlag bool
var colorBlindFlag bool
var autoDetectedWorkspaceDir string // set when auto-detection finds a git repo

// Training data collection flags (opt-in session recording).
var trainFlag bool
var trainEndpoint string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use: "sprout",
	// A runtime failure (e.g. a bad model, a network error) is not a usage
	// mistake, so don't dump the full flag list after it; and don't let cobra
	// print the raw wrapped error itself — Execute() renders one clean line.
	SilenceUsage:  true,
	SilenceErrors: true,
	Short:         "Agent for code analysis and editing (interactive mode when run without arguments)",
	Long: `Sprout is an LLM-powered coding agent for your terminal and browser.

  sprout                      start an interactive session (opens the web UI too)
  sprout agent "your intent"  run one task non-interactively and exit`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		routeGoLogForTerminal()
		// CLI-E: color-blind palette swap. CLI flag wins over env var;
		// ApplyColorBlindFromEnv only sets true (never false) so the
		// flag's explicit `false` isn't clobbered by a stale env.
		if colorBlindFlag {
			console.SetColorBlind(true)
		} else {
			console.ApplyColorBlindFromEnv()
		}
		if debugPprofAddr != "" {
			go func() {
				fmt.Fprintf(os.Stderr, "pprof: listening on http://%s/debug/pprof/\n", debugPprofAddr)
				if err := http.ListenAndServe(debugPprofAddr, nil); err != nil {
					fmt.Fprintf(os.Stderr, "pprof server: %v\n", err)
				}
			}()
		}
		// Auto-detect workspace config when running inside a git repo.
		// Walk up from cwd looking for .git; if found, bootstrap .sprout/
		// on first run and use isolated config. This makes --isolated-config
		// the default for repo-backed directories.
		//
		// Only auto-detect when --isolated-config was not explicitly set
		// (including --isolated-config=false) so users can opt out.
		// Also skip for the system service daemon (SPROUT_SERVICE=1) —
		// the daemon is a system-wide service, not workspace-scoped.
		isolatedFlagExplicit := cmd.Flags().Changed("isolated-config")
		isServiceDaemon := os.Getenv("SPROUT_SERVICE") == "1"
		autoDetected := false
		if !isolatedConfig && !isolatedFlagExplicit && !isServiceDaemon {
			if cwd, err := os.Getwd(); err == nil {
				if isolatedDir, found := detectGitRepo(cwd); found {
					if err := configuration.BootstrapIsolatedConfig(isolatedDir); err == nil {
						isolatedConfig = true
						autoDetected = true
						autoDetectedWorkspaceDir = isolatedDir // for layered config
					}
				}
			}
		}
		if isolatedConfig {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to resolve working directory for --isolated-config: %w", err)
			}
			isolatedDir := filepath.Join(cwd, ".sprout")
			if err := configuration.SetEnv("CONFIG", isolatedDir); err != nil {
				return fmt.Errorf("failed to set SPROUT_CONFIG for --isolated-config: %w", err)
			}
			if err := configuration.BootstrapIsolatedConfig(isolatedDir); err != nil {
				if autoDetected {
					console.GlyphWarning.Printf("Auto-detected a git repo but could not set up its .sprout config: %v", err)
					isolatedConfig = false
				} else {
					return fmt.Errorf("failed to bootstrap isolated config: %w", err)
				}
			}
		}
		return initializeSystem()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default to interactive mode when no arguments provided
		useInteractive := len(args) == 0 && cmd.Flags().NFlag() == 0
		if useInteractive {
			chatAgent, err := createChatAgent()
			if err != nil {
				return fmt.Errorf("failed to initialize agent: %w", err)
			}
			// Use enhanced mode
			return RunAgent(chatAgent, true, args)
		}
		// Otherwise show help
		return cmd.Help()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	applyCommandGroups(rootCmd)
	installUsageErrorHooks(rootCmd)
	if err := rootCmd.Execute(); err != nil {
		// Render exactly one clean line (already-reported errors render
		// nothing — the command showed them while running), then exit with
		// a code scripts can branch on: 2 usage, 130 interrupted, 1 other.
		renderExecuteError(err)
		os.Exit(exitCodeFor(err))
	}
	return nil
}

// initializeSystem initializes configuration and API keys with first-run setup
func initializeSystem() error {
	// Run SP-133 migration if a legacy ~/.sprout directory exists.
	if configuration.NeedsMigration() {
		_ = configuration.RunMigration()
	}
	isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""

	if isCI {
		_, err := configuration.LoadAPIKeys()
		if err != nil && configuration.GetEnvSimple("DEBUG") != "" {
			console.GlyphWarning.Printf("API key initialization: %v", err)
		}
		return nil
	}

	// WebUI-first bootstrap: initialize silently without terminal prompts.
	// First-run setup is completed through the WebUI onboarding flow.
	if _, err := configuration.NewManagerSilent(); err != nil {
		return withHint(fmt.Errorf("initializing configuration: %w", err),
			"Run 'sprout keys set <provider>' to configure an API key, or open the Web UI onboarding.")
	}

	applyTrainingConfig()
	runStartupChecks()
	return nil
}

// applyTrainingConfig resolves training settings from CLI flags and env
// vars, then persists them into the live config manager. The config values
// are later read by the agent to wire the push callback.
//
// Precedence: CLI flag > env var > config.json value.
func applyTrainingConfig() {
	enabled := trainFlag
	endpoint := trainEndpoint

	// Env var fallbacks: SPROUT_TRAIN_ENABLED and SPROUT_TRAIN_ENDPOINT.
	if !enabled {
		if v := configuration.GetEnvSimple("TRAIN_ENABLED"); v == "1" || strings.EqualFold(v, "true") {
			enabled = true
		}
	}
	if endpoint == "" {
		endpoint = configuration.GetEnvSimple("TRAIN_ENDPOINT")
	}

	// Only update config if something was explicitly set via flag or env.
	if !enabled && endpoint == "" {
		return
	}

	mgr, err := configuration.NewManager()
	if err != nil {
		return
	}

	finalEndpoint := endpoint
	finalEnabled := enabled
	_ = mgr.UpdateConfig(func(c *configuration.Config) error {
		if finalEnabled {
			c.Training.Enabled = true
		}
		if finalEndpoint != "" {
			c.Training.Endpoint = finalEndpoint
		}
		return nil
	})

	// Print the warning message showing the effective endpoint.
	cfg := mgr.GetConfig()
	if cfg != nil && cfg.Training.Enabled {
		console.GlyphWarning.Printf("Session recording enabled — conversations are sent to %s", cfg.Training.Endpoint)
	}
}

func runStartupChecks() {
	startupChecksOnce.Do(func() {
		if _, err := pythonruntime.FindPython3Interpreter(); err != nil {
			console.GlyphWarning.Printf("Python-based diff features are unavailable: %v", err)
			return
		}
		if err := tools.CheckPDFPython3Available(); err != nil {
			console.GlyphWarning.Printf("PDF extraction features are unavailable: %v", err)
		}
	})
}

const maxGitWalkDepth = 100

// detectGitRepo walks up from cwd looking for a .git directory.
// Returns the path to the .sprout directory and true if a git repo is found.
// .git files (e.g. submodule references) are not considered directories and
// will not trigger detection.
// Skips detection in CI environments and enforces a depth limit to avoid
// infinite loops on filesystem edge cases.
func detectGitRepo(cwd string) (string, bool) {
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		return "", false
	}
	dir := cwd
	for depth := 0; depth < maxGitWalkDepth; depth++ {
		gitPath := filepath.Join(dir, ".git")
		info, err := os.Stat(gitPath)
		if err == nil && info.IsDir() {
			return filepath.Join(dir, ".sprout"), true
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached filesystem root
			break
		}
		dir = parent
	}
	return "", false
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be available to all subcommands in the application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/sprout/config.json)")
	rootCmd.PersistentFlags().BoolVar(&isolatedConfig, "isolated-config", false, "Use per-working-directory config at ./.sprout (clone from main config on first run)")
	rootCmd.PersistentFlags().StringVar(&debugPprofAddr, "debug-pprof", "", "If set, start a pprof HTTP server on this address (e.g. localhost:6060) for live memory/CPU profiling")
	rootCmd.PersistentFlags().BoolVar(&whyFlag, "why", false, "Print detailed risk assessment on security errors")
	rootCmd.PersistentFlags().BoolVar(&colorBlindFlag, "color-blind", false, "Swap the success/error/warning palette to a deuteranopia / protanopia-friendly scheme (also honors SPROUT_COLOR_BLIND=1)")
	rootCmd.PersistentFlags().BoolVar(&trainFlag, "train", false, "Enable session recording for training data collection (OFF by default; also settable via SPROUT_TRAIN_ENABLED=true)")
	rootCmd.PersistentFlags().StringVar(&trainEndpoint, "train-endpoint", "", "Training data collection endpoint URL (also settable via SPROUT_TRAIN_ENDPOINT)")

	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(exportTrainingCmd)
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(txnStatusCmd)
	rootCmd.AddCommand(txnPushCmd)
	rootCmd.AddCommand(txnPullCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(customModelCmd)
	rootCmd.AddCommand(reviewStagedCmd)
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(historyCmd)
	rootCmd.AddCommand(automateCmd)
	rootCmd.AddCommand(shellBgCmd)
	rootCmd.AddCommand(prCmd)
}
