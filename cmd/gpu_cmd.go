package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/gpulink"
	"github.com/sprout-foundry/sprout/pkg/runner"
)

var gpuCmd = &cobra.Command{
	Use:   "gpu",
	Short: "Link a GPU you run to your platform account",
	Long: `Link an inference engine this machine runs (Ollama, llama.cpp, vLLM, MLX,
LM Studio: anything that answers OpenAI's /v1/chat/completions) to your
platform account, so your tasks and Sprout can run on its models.

The agent connects out to the platform: no port is opened on your network,
and the platform reaches only the engine's model-serving paths.`,
}

var (
	gpuEngine    string
	gpuEngineKey string
	gpuName      string
	gpuPlatform  string
	gpuNoBrowser bool
)

func init() {
	linkCmd := &cobra.Command{
		Use:   "link",
		Short: "Link this machine's engine to your platform account",
		Args:  cobra.NoArgs,
		RunE:  runGPULink,
	}
	linkCmd.Flags().StringVar(&gpuEngine, "engine", gpulink.DefaultEngine, "host:port of the engine as this machine reaches it (Ollama 11434, llama.cpp 8080, vLLM 8000, LM Studio 1234)")
	linkCmd.Flags().StringVar(&gpuEngineKey, "engine-key", "", "the engine's own API key, if it requires one (stored in the credential store)")
	linkCmd.Flags().StringVar(&gpuName, "name", "", "name shown on the platform (default: this machine's hostname)")
	linkCmd.Flags().StringVar(&gpuPlatform, "platform", "", "platform URL (default $SPROUT_PLATFORM_URL, else the previous link, else "+runner.DefaultPlatformURL+")")
	linkCmd.Flags().BoolVar(&gpuNoBrowser, "no-browser", false, "print the approval URL without opening a browser (SSH sessions, headless machines)")

	gpuCmd.AddCommand(
		linkCmd,
		&cobra.Command{Use: "start", Short: "Serve the platform's requests to the engine (foreground)", Args: cobra.NoArgs, RunE: runGPUStart},
		&cobra.Command{Use: "status", Short: "Show the link and whether the engine answers", Args: cobra.NoArgs, RunE: runGPUStatus},
		&cobra.Command{Use: "install", Short: "Run the GPU link at login (systemd user unit or launchd agent)", Args: cobra.NoArgs, RunE: runGPUInstall},
		&cobra.Command{Use: "uninstall", Short: "Stop running the GPU link at login", Args: cobra.NoArgs, RunE: runGPUUninstall},
		&cobra.Command{Use: "unlink", Short: "Forget the link on this machine", Args: cobra.NoArgs, RunE: runGPUUnlink},
	)
	rootCmd.AddCommand(gpuCmd)
}

// validEngine checks an engine address is host:port.
func validEngine(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("--engine must be host:port, e.g. %s (got %q)", gpulink.DefaultEngine, addr)
	}
	return nil
}

func runGPULink(cmd *cobra.Command, _ []string) error {
	st, err := gpulink.LoadState()
	if err != nil {
		return err
	}
	platform := resolveLinkPlatform(gpuPlatform, os.Getenv("SPROUT_PLATFORM_URL"), st.PlatformURL)
	if u, err := url.Parse(platform); err != nil || (u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		return fmt.Errorf("platform URL must be https (got %q)", platform)
	}
	if err := validEngine(gpuEngine); err != nil {
		return err
	}
	name := gpuName
	if name == "" {
		name, _ = os.Hostname()
	}
	out := cmd.OutOrStdout()
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The engine is checked first, so a typo in the address shows here
	// rather than as a GPU that never answers.
	engine := gpuEngine
	if models, err := gpulink.EngineModels(ctx, gpuEngine, gpuEngineKey); err != nil {
		_, _ = fmt.Fprintf(out, "Note: the engine at %s did not answer /v1/models (%v). Linking anyway; start it before using the GPU.\n\n", gpuEngine, err)
	} else if len(models) > 0 {
		_, _ = fmt.Fprintf(out, "The engine at %s serves: %s\n\n", gpuEngine, strings.Join(models, ", "))
		engine = gpuEngine + " (" + strings.Join(models, ", ") + ")"
	}

	client := gpulink.NewClient(platform)
	start, err := client.StartLink(ctx, name, runtime.GOOS, runtime.GOARCH, engine)
	if err != nil {
		return err
	}
	verify := firstNonEmpty(start.VerificationURIComplete, start.VerificationURI)
	_, _ = fmt.Fprintf(out, "To link %q, approve it on the platform:\n\n  %s\n\nand check the code matches: %s\n\nWaiting for approval…\n", name, verify, start.UserCode)
	if !gpuNoBrowser {
		runner.OpenURL(verify)
	}
	creds, err := pollGPULink(ctx, client, start)
	if err != nil {
		return err
	}
	keyStore, err := gpulink.SaveAPIKey(creds.APIKey)
	if err != nil {
		return fmt.Errorf("storing the GPU link key failed (%w); remove %q under Account → My GPUs and link again", err, name)
	}
	if err := gpulink.SaveEngineKey(gpuEngineKey); err != nil {
		return fmt.Errorf("storing the engine key: %w", err)
	}
	st.PlatformURL = strings.TrimRight(platform, "/")
	st.LinkID = creds.LinkID
	st.Name = name
	st.Engine = gpuEngine
	if err := st.Save(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Linked as %q (key stored in the %s). Start it with `sprout gpu start`, or `sprout gpu install` to run it at login. Then choose one of its models under Account → Models.\n", name, keyStore)
	return nil
}

func pollGPULink(ctx context.Context, client *gpulink.Client, start *runner.DeviceStart) (*gpulink.Credentials, error) {
	interval := time.Duration(start.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		creds, err := client.PollLink(ctx, start.DeviceCode)
		switch {
		case err == nil:
			return creds, nil
		case errors.Is(err, runner.ErrAuthorizationPending):
		case errors.Is(err, runner.ErrSlowDown):
			interval += 5 * time.Second
		default:
			return nil, err
		}
		if start.ExpiresIn > 0 && time.Now().After(deadline) {
			return nil, runner.ErrExpiredToken
		}
	}
}

func loadLinkedGPU() (*gpulink.State, string, error) {
	st, err := gpulink.LoadState()
	if err != nil {
		return nil, "", err
	}
	if !st.Linked() {
		return nil, "", errors.New("no GPU is linked on this machine; run `sprout gpu link` first")
	}
	key, err := gpulink.LoadAPIKey()
	if err != nil || key == "" {
		return nil, "", errors.New("the GPU link key is missing from the credential store; run `sprout gpu link` again")
	}
	return st, key, nil
}

func runGPUStart(cmd *cobra.Command, _ []string) error {
	st, key, err := loadLinkedGPU()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil)).With("gpu", st.Name)
	agent := &gpulink.Agent{State: st, APIKey: key, EngineKey: gpulink.LoadEngineKey(), Log: log}
	log.Info("starting", "platform", st.PlatformURL, "engine", st.Engine)
	return agent.Run(ctx)
}

func runGPUStatus(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	st, err := gpulink.LoadState()
	if err != nil {
		return err
	}
	if !st.Linked() {
		_, _ = fmt.Fprintln(out, "No GPU is linked on this machine. Run `sprout gpu link --engine host:port`.")
		return nil
	}
	_, _ = fmt.Fprintf(out, "Linked as %q to %s\nEngine: %s\n", st.Name, st.PlatformURL, st.Engine)
	models, err := gpulink.EngineModels(cmd.Context(), st.Engine, gpulink.LoadEngineKey())
	if err != nil {
		_, _ = fmt.Fprintf(out, "The engine is not answering: %v\n", err)
		return nil
	}
	_, _ = fmt.Fprintf(out, "The engine serves: %s\n", strings.Join(models, ", "))
	return nil
}

func runGPUInstall(cmd *cobra.Command, _ []string) error {
	if _, _, err := loadLinkedGPU(); err != nil {
		return err
	}
	path, err := gpulink.InstallService()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Installed %s; the GPU link runs at login.\n", path)
	if gpulink.ServiceNote != "" {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), gpulink.ServiceNote)
	}
	return nil
}

func runGPUUninstall(cmd *cobra.Command, _ []string) error {
	if err := gpulink.UninstallService(); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "The GPU link no longer runs at login.")
	return nil
}

func runGPUUnlink(cmd *cobra.Command, _ []string) error {
	st, err := gpulink.LoadState()
	if err != nil {
		return err
	}
	_ = gpulink.UninstallService()
	if err := gpulink.Remove(); err != nil {
		return err
	}
	name := st.Name
	if name == "" {
		name = "this GPU"
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Forgot the link on this machine. Remove %q under Account → My GPUs on the platform too.\n", name)
	return nil
}
