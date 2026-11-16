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
	"github.com/sprout-foundry/sprout/pkg/platformauth"
	"github.com/sprout-foundry/sprout/pkg/runner"
)

var gpuCmd = &cobra.Command{
	Use:   "gpu",
	Short: "Link a GPU you run to your platform account",
	Long: `Link an inference engine this machine runs (Ollama, llama.cpp, vLLM, MLX,
LM Studio: anything that answers OpenAI's /v1/chat/completions) to your
platform account, so your tasks and Sprout can run on its models. On an
Apple Silicon Mac, sprout can serve the model itself with sinter (--sinter);
it does so too when no --engine is given and nothing answers at Ollama's port.

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
	linkCmd.Flags().StringVar(&gpuName, "name", "", "name shown on the platform (default: this machine's hostname)")
	linkCmd.Flags().StringVar(&gpuPlatform, "platform", "", "platform URL (default $SPROUT_PLATFORM_URL, else the previous link, else "+runner.DefaultPlatformURL+")")
	linkCmd.Flags().BoolVar(&gpuNoBrowser, "no-browser", false, "print the approval URL without opening a browser (SSH sessions, headless machines)")
	linkCmd.Flags().BoolVar(&gpuSinter, "sinter", false, "serve a model in-process with sinter instead of a server at --engine (Apple Silicon)")
	linkCmd.Flags().BoolVar(&gpuFleet, "fleet", false, "link to the platform's own GPU fleet (a machine the platform owns or rents), approved by a platform admin, instead of to your account")
	linkCmd.Flags().StringVar(&gpuToken, "token", "", "an enrollment token an admin minted (Admin → Linked GPUs): link to the fleet without the browser approval, for a rented box that links itself; implies --fleet")
	linkCmd.Flags().StringVar(&gpuModel, "model", "", "the model to serve with sinter (a catalog name such as qwen3.5-9b, an installed model, or a model directory; default: the one recommended for this machine), downloaded if missing; implies --sinter")

	gpuCmd.AddCommand(
		linkCmd,
		&cobra.Command{Use: "start", Short: "Serve the platform's requests to the engine (foreground)", Args: cobra.NoArgs, RunE: runGPUStart},
		&cobra.Command{Use: "status", Short: "Show the link and whether the engine answers", Args: cobra.NoArgs, RunE: runGPUStatus},
		&cobra.Command{Use: "install", Short: "Run the GPU link at login (systemd user unit or launchd agent)", Args: cobra.NoArgs, RunE: runGPUInstall},
		&cobra.Command{Use: "uninstall", Short: "Stop running the GPU link at login", Args: cobra.NoArgs, RunE: runGPUUninstall},
		&cobra.Command{Use: "unlink", Short: "Forget the link on this machine", Args: cobra.NoArgs, RunE: runGPUUnlink},
		&cobra.Command{Use: "rotate-key", Short: "Trade the link's key for a new one (a running agent moves to it when it restarts)", Args: cobra.NoArgs, RunE: runGPURotateKey},
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
		name, _ = os.Hostname()
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
	st.Account = creds.AccountEmail
	st.Engine = gpuEngine
	st.Model = ""
	st.Fleet = gpuFleet
	st.KeyRotatedAt = time.Now().UTC()
	if useSinter {
		st.Engine = gpulink.EngineSinter
		st.Model = sinterDir
	}
	if err := st.Save(); err != nil {
		return err
	}
	if gpuFleet {
		_, _ = fmt.Fprintf(out, "Linked %q to the platform fleet (key stored in the %s). Start it with `sprout gpu start`, or `sprout gpu install` to run it at login. Then add it as a Linked GPU (fleet) provider under Admin → LLM providers.\n", name, keyStore)
		return nil
	}
	_, _ = fmt.Fprintf(out, "Linked as %q%s (key stored in the %s). Start it with `sprout gpu start`, or `sprout gpu install` to run it at login. Then choose one of its models under Account → Models.\n", name, platformauth.LinkedTo(creds.AccountEmail), keyStore)
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
	agent := &gpulink.Agent{State: st, APIKey: key, EngineKey: gpulink.LoadEngineKey(), Log: log, ReloadKey: gpulink.LoadAPIKey}
	if st.Fleet {
		// A fleet GPU keeps a tunnel to each of two platform replicas and
		// rotates its key monthly.
		agent.Tunnels = 2
		agent.RotateEvery = gpulink.FleetKeyRotation
		agent.RotatedAt = st.KeyRotatedAt
		agent.SaveKey = func(k string) error { _, err := gpulink.SaveAPIKey(k); return err }
		agent.OnRotated = func(at time.Time) {
			st.KeyRotatedAt = at
			if err := st.Save(); err != nil {
				log.Warn("recording the key rotation", "err", err)
			}
		}
	}
	if st.Engine == gpulink.EngineSinter {
		handler, model, unload, err := gpulink.SinterHandler(st.Model)
		if err != nil {
			return err
		}
		defer unload()
		agent.Local = handler
		log.Info("serving with sinter", "model", model)
	} else if _, err := gpulink.EngineModels(ctx, st.Engine, agent.EngineKey); err != nil {
		// The tunnel still opens: the engine may start later, and the
		// platform picks its models up within a minute.
		log.Warn("the engine is not answering; requests fail until it starts (or link again with `sprout gpu link --sinter`)", "engine", st.Engine, "err", err)
	}
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
	_, _ = fmt.Fprintf(out, "Linked as %q to %s\nAccount: %s\nEngine: %s\n", st.Name, st.PlatformURL, firstNonEmpty(st.Account, "(not recorded; link again to see it)"), st.Engine)
	if st.Engine == gpulink.EngineSinter {
		_, _ = fmt.Fprintf(out, "Model: %s (served in-process by `sprout gpu start`)\n", st.Model)
		return nil
	}
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

func runGPURotateKey(cmd *cobra.Command, _ []string) error {
	st, key, err := loadLinkedGPU()
	if err != nil {
		return err
	}
	next, err := gpulink.RotateKey(cmd.Context(), st.PlatformURL, st.LinkID, key)
	if err != nil {
		return err
	}
	keyStore, err := gpulink.SaveAPIKey(next)
	if err != nil {
		// The platform keeps the current key working until the new one is
		// used, so nothing is lost.
		return fmt.Errorf("storing the new key failed (%w); the current key keeps working", err)
	}
	st.KeyRotatedAt = time.Now().UTC()
	if err := st.Save(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Rotated the key for %q (stored in the %s). A running agent keeps the old key until it restarts; the old key stops working once the new one connects.\n", st.Name, keyStore)
	return nil
}
