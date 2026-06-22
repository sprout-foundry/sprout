//go:build !js

package cmd

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/pythonruntime"
)

var startupChecksOnce sync.Once
var isolatedConfig bool
var debugPprofAddr string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use: "sprout",
	// A runtime failure (e.g. a bad model, a network error) is not a usage
	// mistake, so don't dump the full flag list after it; and don't let cobra
	// print the raw wrapped error itself — Execute() renders one clean line.
	SilenceUsage:  true,
	SilenceErrors: true,
	Short:         "Agent for code analysis and editing (interactive mode when run without arguments)",
	Long: `Sprout is a command-line tool that leverages Large Language Models (LLMs)
to automate and assist in software development tasks. It features a modern CLI
with automatic web UI startup for rich interactive experiences.

For autonomous operation, try: sprout agent "your intent here"

Running just 'sprout' without arguments starts enhanced agent mode with automatic web UI.

See "Available Commands" below for the full list.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if debugPprofAddr != "" {
			go func() {
				fmt.Fprintf(os.Stderr, "pprof: listening on http://%s/debug/pprof/\n", debugPprofAddr)
				if err := http.ListenAndServe(debugPprofAddr, nil); err != nil {
					fmt.Fprintf(os.Stderr, "pprof server: %v\n", err)
				}
			}()
		}
		if isolatedConfig {
			cwd, err := os.Getwd()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to resolve working directory for --isolated-config: %v\n", err)
				os.Exit(1)
			}
			isolatedDir := filepath.Join(cwd, ".sprout")
			if err := configuration.SetEnv("CONFIG", isolatedDir); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to set SPROUT_CONFIG for --isolated-config: %v\n", err)
				os.Exit(1)
			}
			if err := configuration.BootstrapIsolatedConfig(isolatedDir); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to bootstrap isolated config: %v\n", err)
				os.Exit(1)
			}
		}
		// Initialize API keys and configuration
		initializeSystem()
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
	if err := rootCmd.Execute(); err != nil {
		// Render exactly one clean line (already-reported errors render
		// nothing — the command showed them while running), then exit
		// non-zero so shells and CI see the failure.
		renderExecuteError(err)
		os.Exit(1)
	}
	return nil
}

// initializeSystem initializes configuration and API keys with first-run setup
func initializeSystem() {
	// Check if we're in a CI environment or non-interactive mode
	isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""

	if isCI {
		// In CI environments, just load what we can and continue
		_, err := configuration.LoadAPIKeys()
		if err != nil && configuration.GetEnvSimple("DEBUG") != "" {
			println("API key initialization warning:", err.Error())
		}
		return
	}

	// WebUI-first bootstrap: initialize silently without terminal prompts.
	// First-run setup is completed through the WebUI onboarding flow.
	_, err := configuration.NewManagerSilent()
	if err != nil {
		// If initialization fails, print helpful error and exit
		fmt.Fprintf(os.Stderr, "Failed to initialize sprout: %v\n", err)
		fmt.Fprintln(os.Stderr, "\nThis usually means there's an issue with your configuration or API keys.")
		fmt.Fprintln(os.Stderr, "   Try opening the Web UI onboarding or checking ~/.config/sprout configuration.")
		os.Exit(1)
	}

	runStartupChecks()
}

func runStartupChecks() {
	startupChecksOnce.Do(func() {
		if _, err := pythonruntime.FindPython3Interpreter(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Python-based diff features are unavailable: %v\n", err)
			return
		}
		if err := tools.CheckPDFPython3Available(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: PDF extraction features are unavailable: %v\n", err)
		}
	})
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be available to all subcommands in the application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/sprout/config.json)")
	rootCmd.PersistentFlags().BoolVar(&isolatedConfig, "isolated-config", false, "Use per-working-directory config at ./.sprout (clone from main config on first run)")
	rootCmd.PersistentFlags().StringVar(&debugPprofAddr, "debug-pprof", "", "If set, start a pprof HTTP server on this address (e.g. localhost:6060) for live memory/CPU profiling")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(exportTrainingCmd)
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(customModelCmd)
	rootCmd.AddCommand(reviewStagedCmd)
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(historyCmd)
	rootCmd.AddCommand(automateCmd)
	rootCmd.AddCommand(prCmd)
}
