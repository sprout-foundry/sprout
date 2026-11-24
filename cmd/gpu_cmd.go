package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/sprout-foundry/sprout/pkg/platformauth"
	"github.com/sprout-foundry/sprout/pkg/runner"
)

var gpuCmd = &cobra.Command{
	Use:   "gpu",
	Short: "Link GPUs you run to your platform account",
	Long: `Link inference engines this machine runs (Ollama, llama.cpp, vLLM, MLX,
LM Studio: anything that answers OpenAI's /v1/chat/completions) to your
platform account — several per machine, one background service per link. On
an Apple Silicon Mac, sprout can serve the model itself with sinter
(--sinter); it does so too when no --engine is given and nothing answers at
Ollama's port.

The agent connects out to the platform: no port is opened on your network,
and the platform reaches only the engine's model-serving paths.`,
}

var (
	gpuEngine    string
	gpuEngineKey string
	gpuName      string
	gpuPlatform  string
	gpuNoBrowser bool
	gpuSinter    bool
	gpuModel     string
	gpuFleet     bool
	gpuToken     string
	gpuNoService bool
)

func init() {
	linkCmd := &cobra.Command{
		Use:   "link",
		Short: "Link this machine's engine to your platform account",
		Args:  cobra.NoArgs,
		RunE:  runGPULink,
	}
	linkCmd.Flags().StringVar(&gpuEngine, "engine", gpulink.DefaultEngine, "host:port of the engine as this machine reaches it (Ollama 11434, llama.cpp 8080, vLLM 8000, LM Studio 1234); when not given and nothing answers there, Apple Silicon Macs serve with sinter")
	linkCmd.Flags().StringVar(&gpuEngineKey, "engine-key", "", "the engine's own API key, if it requires one (stored in the credential store)")
	linkCmd.Flags().StringVar(&gpuName, "name", "", "name shown on the platform (default: this machine's hostname, or hostname-port when that name is already linked here)")
	linkCmd.Flags().StringVar(&gpuPlatform, "platform", "", "platform URL (default $SPROUT_PLATFORM_URL, else an existing link's, else "+runner.DefaultPlatformURL+")")
	linkCmd.Flags().BoolVar(&gpuNoBrowser, "no-browser", false, "print the approval URL without opening a browser (SSH sessions, headless machines)")
	linkCmd.Flags().BoolVar(&gpuSinter, "sinter", false, "serve a model in-process with sinter instead of a server at --engine (Apple Silicon)")
	linkCmd.Flags().BoolVar(&gpuFleet, "fleet", false, "link to the platform's own GPU fleet (a machine the platform owns or rents), approved by a platform admin, instead of to your account")
	linkCmd.Flags().BoolVar(&gpuNoService, "no-service", false, "link only: do not install and start the background service (run `sprout gpu start` yourself)")
	linkCmd.Flags().StringVar(&gpuToken, "token", "", "an enrollment token an admin minted (Admin → Linked GPUs): link to the fleet without the browser approval, for a rented box that links itself; implies --fleet")
	linkCmd.Flags().StringVar(&gpuModel, "model", "", "the model to serve with sinter (a catalog name such as qwen3.5-9b, an installed model, or a model directory; default: the one recommended for this machine), downloaded if missing; implies --sinter")

	gpuCmd.AddCommand(
		linkCmd,
		&cobra.Command{Use: "start", Short: "Serve one link's requests to the engine in the foreground (what the background service runs)", Args: cobra.NoArgs, RunE: runGPUStart},
		&cobra.Command{Use: "status", Short: "Show every link and whether its engine answers", Args: cobra.NoArgs, RunE: runGPUStatus},
		&cobra.Command{Use: "install", Short: "Run the GPU links in the background, at login and after restarts (one systemd user-unit instance or launchd agent per link); `link` does this already", Args: cobra.NoArgs, RunE: runGPUInstall},
		&cobra.Command{Use: "uninstall", Short: "Stop running the GPU links at login (all of them, or one with --name)", Args: cobra.NoArgs, RunE: runGPUUninstall},
		&cobra.Command{Use: "unlink", Short: "Forget one link on this machine", Args: cobra.NoArgs, RunE: runGPUUnlink},
		&cobra.Command{Use: "rotate-key", Short: "Trade a link's key for a new one (a running agent moves to it when it restarts)", Args: cobra.NoArgs, RunE: runGPURotateKey},
	)
	for _, c := range gpuCmd.Commands() {
		switch c.Name() {
		case "start", "unlink", "rotate-key", "uninstall":
			c.Flags().StringVar(&gpuName, "name", "", "which link (needed when several are linked; see `sprout gpu status`)")
		case "install":
			c.Flags().StringVar(&gpuName, "name", "", "install only this link's service (default: every link's)")
		}
	}
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
	existing, err := gpulink.LoadLinks()
	if err != nil {
		return err
	}
	platform := gpuLinkPlatform(existing)
	if u, err := url.Parse(platform); err != nil || (u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		return fmt.Errorf("platform URL must be https (got %q)", platform)
	}
	engineSet := cmd.Flags().Changed("engine")
	useSinter := gpuSinter || (gpuModel != "" && !engineSet)
	if !useSinter {
		if err := validEngine(gpuEngine); err != nil {
			return err
		}
	} else if engineSet {
		return errors.New("--sinter serves a model in-process; pass --model instead of --engine")
	}
	if gpuToken != "" {
		gpuFleet = true
	}
	name := gpuName
	if name == "" {
		name = gpulink.DefaultLinkName(existing, gpuEngine)
	}
	slot := gpulink.Slot(name)
	if slot == "" {
		return fmt.Errorf("link name %q has no usable file name; pick another with --name", name)
	}
	for _, old := range existing {
		if old.Slot() == slot {
			return fmt.Errorf("%q is already linked here as %q (engine %s); pass a different --name, or unlink it first with `sprout gpu unlink --name %s`", name, old.Name, old.Engine, slot)
		}
	}
	out := cmd.OutOrStdout()
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The engine is checked (or, with sinter, its model fetched) first, so
	// a typo shows here rather than as a GPU that never answers.
	engine := gpuEngine
	sinterDir := ""
	var engineModels []string
	var engineErr error
	if !useSinter {
		engineModels, engineErr = gpulink.EngineModels(ctx, gpuEngine, gpuEngineKey)
		if engineErr != nil && !engineSet && gpulink.SinterAvailable {
			// No engine was named and none runs at Ollama's port: serve
			// in-process rather than link a GPU that cannot answer.
			_, _ = fmt.Fprintf(out, "Nothing answers at %s (Ollama's port), so this links a model served in-process with sinter.\nTo link a server you run instead, start it and pass --engine host:port.\n\n", gpuEngine)
			useSinter = true
		}
	}
	if useSinter {
		dir, model, err := prepareSinterModel(ctx, out, gpuModel)
		if err != nil {
			return err
		}
		sinterDir = dir
		engine = gpulink.EngineSinter + " (" + model + ")"
		_, _ = fmt.Fprintf(out, "Serving %s in-process with sinter.\n\n", model)
	} else if engineErr != nil {
		_, _ = fmt.Fprintf(out, "Note: the engine at %s did not answer /v1/models (%v). Linking anyway; start it before using the GPU.\n\n", gpuEngine, engineErr)
	} else if len(engineModels) > 0 {
		_, _ = fmt.Fprintf(out, "The engine at %s serves: %s\n\n", gpuEngine, strings.Join(engineModels, ", "))
		engine = gpuEngine + " (" + strings.Join(engineModels, ", ") + ")"
	}

	var creds *gpulink.Credentials
	if gpuToken != "" {
		// An admin's enrollment token stands in for the browser approval.
		if creds, err = gpulink.NewClient(platform).Enroll(ctx, gpuToken, name, runtime.GOOS, runtime.GOARCH, engine); err != nil {
			return err
		}
	} else if _, token, ok := platformauth.SignedInTo(platform); ok && !gpuFleet {
		// Signed in (`sprout login`): link straight to that account.
		linked, err := platformauth.NewClient(platform, token).LinkGPU(ctx, platformauth.LinkRequest{Name: name, OS: runtime.GOOS, Arch: runtime.GOARCH, Engine: engine})
		if err != nil {
			return err
		}
		creds = &gpulink.Credentials{LinkID: linked.LinkID, APIKey: linked.APIKey, AccountEmail: linked.AccountEmail}
	} else {
		// A fleet link always takes the browser approval: only a platform
		// admin, with a fresh second factor, may add a machine to the fleet.
		client := gpulink.NewClient(platform)
		var start *runner.DeviceStart
		if gpuFleet {
			start, err = client.StartFleetLink(ctx, name, runtime.GOOS, runtime.GOARCH, engine)
		} else {
			start, err = client.StartLink(ctx, name, runtime.GOOS, runtime.GOARCH, engine)
		}
		if err != nil {
			return err
		}
		verify := start.VerificationURI
		_, _ = fmt.Fprintf(out, "To link %q, approve it on the platform: open\n\n  %s\n\nand enter this code: %s\n\nWaiting for approval…\n", name, verify, start.UserCode)
		if !gpuNoBrowser {
			runner.OpenURL(verify)
		}
		if creds, err = pollGPULink(ctx, client, start); err != nil {
			return err
		}
	}
	keyStore, err := gpulink.SaveAPIKeyFor(slot, creds.APIKey)
	if err != nil {
		return fmt.Errorf("storing the GPU link key failed (%w); remove %q under Account → My GPUs and link again", err, name)
	}
	if err := gpulink.SaveEngineKeyFor(slot, gpuEngineKey); err != nil {
		return fmt.Errorf("storing the engine key: %w", err)
	}
	st := &gpulink.State{
		PlatformURL:  strings.TrimRight(platform, "/"),
		LinkID:       creds.LinkID,
		Name:         name,
		Account:      creds.AccountEmail,
		Engine:       gpuEngine,
		Fleet:        gpuFleet,
		KeyRotatedAt: time.Now().UTC(),
	}
	if useSinter {
		st.Engine = gpulink.EngineSinter
		st.Model = sinterDir
	}
	if err := st.Save(); err != nil {
		return err
	}
	if gpuFleet {
		_, _ = fmt.Fprintf(out, "Linked %q to the platform fleet (key stored in the %s).\n", name, keyStore)
	} else {
		_, _ = fmt.Fprintf(out, "Linked as %q%s (key stored in the %s).\n", name, platformauth.LinkedTo(creds.AccountEmail), keyStore)
	}
	startGPUService(out, st)
	if gpuFleet {
		_, _ = fmt.Fprintln(out, "Then add it as a Linked GPU (fleet) provider under Admin → LLM providers.")
	} else {
		_, _ = fmt.Fprintln(out, "Then choose one of its models under Account → Models.")
	}
	return nil
}

// gpuLinkPlatform resolves the platform URL: --platform, then
// $SPROUT_PLATFORM_URL, then an existing link's, then the default.
func gpuLinkPlatform(existing []*gpulink.State) string {
	saved := ""
	for _, st := range existing {
		if st.PlatformURL != "" {
			saved = st.PlatformURL
			break
		}
	}
	return resolveLinkPlatform(gpuPlatform, os.Getenv("SPROUT_PLATFORM_URL"), saved)
}

// startGPUService runs the link in the background (the login service),
// unless --no-service, or says how to when this OS has no service.
func startGPUService(out io.Writer, st *gpulink.State) {
	if gpuNoService {
		_, _ = fmt.Fprintf(out, "Start it with `sprout gpu start --name %s`, or `sprout gpu install` to run every link in the background.\n", st.Slot())
		return
	}
	path, err := gpulink.InstallService(st.Name)
	if err != nil {
		_, _ = fmt.Fprintf(out, "Could not run it in the background (%v); start it with `sprout gpu start --name %s`.\n", err, st.Slot())
		return
	}
	_, _ = fmt.Fprintf(out, "Running in the background (%s), at login and after restarts; `sprout gpu status` shows it.\n", path)
	if note := gpulink.ServiceNotice(); note != "" {
		_, _ = fmt.Fprintln(out, note)
	}
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

// gpuTarget resolves --name to one saved link (the only one when no name is
// given) with its credential slot.
func gpuTarget() (*gpulink.State, string, error) {
	links, err := gpulink.LoadLinks()
	if err != nil {
		return nil, "", err
	}
	if len(links) == 0 {
		return nil, "", errors.New("no GPU is linked on this machine; run `sprout gpu link` first")
	}
	if gpuName == "" {
		if len(links) > 1 {
			return nil, "", errors.New("several GPUs are linked on this machine; pass --name (see `sprout gpu status`)")
		}
		return links[0], links[0].Slot(), nil
	}
	slot := gpulink.Slot(gpuName)
	for _, st := range links {
		if st.Slot() == slot {
			return st, slot, nil
		}
	}
	return nil, "", fmt.Errorf("no GPU is linked under %q; see `sprout gpu status`", gpuName)
}

// gpuAPIKey reads a link's agent key, saying what to do when it is gone.
func gpuAPIKey(st *gpulink.State, slot string) (string, error) {
	key, err := gpulink.LoadAPIKeyFor(slot)
	if err != nil || key == "" {
		return "", fmt.Errorf("the key for %q is missing from the credential store; run `sprout gpu link` again", st.Name)
	}
	return key, nil
}

// isGPUCommand reports whether cmd is `sprout gpu` or one of its subcommands.
func isGPUCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c == gpuCmd {
			return true
		}
	}
	return false
}
