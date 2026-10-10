package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/runner"
	"github.com/sprout-foundry/sprout/pkg/runner/sandbox"
)

var runnerCmd = &cobra.Command{
	Use:   "runner",
	Short: "Use this machine as a runner for the browser build",
	Long: `Link this machine to your platform account so the browser build's
escalated commands (builds, tests, git push) run here instead of in a cloud
workspace.

Modes:
  container   each workspace runs in a hardened container (default)
  native      host toolchains under the OS sandbox (Seatbelt, bubblewrap)
  bare-metal  host toolchains with no sandbox — your full user access`,
}

var (
	runnerPlatform   string
	runnerName       string
	runnerPublicURL  string
	runnerListen     string
	runnerNoBrowser  bool
	runnerWorkspaces []string
	runnerStartLocal []string
	runnerImage      string
)

var imageFlagUsage = "workspace image container mode runs (saved; default " + runner.DefaultImage() + "); the runner pulls it with Docker's own credentials, so run `docker login <registry>` for a private image"

func init() {
	linkCmd := &cobra.Command{
		Use:   "link",
		Short: "Link this machine to your platform account",
		RunE:  runRunnerLink,
	}
	linkCmd.Flags().StringVar(&runnerPlatform, "platform", "", "platform URL (default $SPROUT_PLATFORM_URL)")
	linkCmd.Flags().StringVar(&runnerName, "name", "", "name shown on the platform (default: this machine's hostname)")
	linkCmd.Flags().StringVar(&runnerPublicURL, "public-url", "", "HTTPS URL the platform reaches this runner at; omit to connect through the platform relay")
	linkCmd.Flags().StringVar(&runnerListen, "listen", "", "address the runner listens on (default "+runner.DefaultListenAddr+")")
	linkCmd.Flags().BoolVar(&runnerNoBrowser, "no-browser", false, "print the approval URL without opening a browser (SSH sessions, headless machines)")
	linkCmd.Flags().StringArrayVar(&runnerWorkspaces, "dir", nil, "local directory this runner serves workspaces in place (repeatable); the user's real files are the workspace — opt in deliberately")
	linkCmd.Flags().StringArrayVar(&runnerWorkspaces, "workspace", nil, "alias for --dir")
	markAlias(linkCmd.Flags(), "workspace", "dir", aliasDeprecated)
	linkCmd.Flags().StringVar(&runnerImage, "image", "", imageFlagUsage)

	startCmd := &cobra.Command{Use: "start", Short: "Run the runner in the foreground", Args: cobra.NoArgs, RunE: runRunnerStart}
	startCmd.Flags().StringArrayVar(&runnerStartLocal, "dir", nil, "local directory this runner serves workspaces in place (repeatable; passing it replaces the saved list); the user's real files are the workspace — opt in deliberately")
	startCmd.Flags().StringArrayVar(&runnerStartLocal, "workspace", nil, "alias for --dir")
	markAlias(startCmd.Flags(), "workspace", "dir", aliasDeprecated)
	startCmd.Flags().StringVar(&runnerImage, "image", "", imageFlagUsage)

	runnerCmd.AddCommand(
		linkCmd,
		startCmd,
		&cobra.Command{Use: "status", Short: "Show link state, mode and sandbox", Args: cobra.NoArgs, RunE: runRunnerStatus},
		&cobra.Command{
			Use:       "mode container|native|bare-metal",
			Short:     "Choose how workspaces run on this machine",
			Args:      cobra.ExactArgs(1),
			ValidArgs: []string{runner.ModeContainer, runner.ModeNative, runner.ModeBareMetal},
			RunE:      runRunnerMode,
		},
		&cobra.Command{Use: "unlink", Short: "Forget this machine's link", Args: cobra.NoArgs, RunE: runRunnerUnlink},
		&cobra.Command{Use: "install", Short: "Start the runner at login (launchd / systemd)", Args: cobra.NoArgs, RunE: runRunnerInstall},
		&cobra.Command{Use: "uninstall", Short: "Remove the login service", Args: cobra.NoArgs, RunE: runRunnerUninstall},
	)
	rootCmd.AddCommand(runnerCmd)
}

func runRunnerLink(cmd *cobra.Command, _ []string) error {
	st, err := runner.LoadState()
	if err != nil {
		return err
	}
	platform := firstNonEmpty(runnerPlatform, os.Getenv("SPROUT_PLATFORM_URL"), st.PlatformURL)
	if platform == "" {
		return errors.New("which platform? pass --platform https://… or set SPROUT_PLATFORM_URL")
	}
	if u, err := url.Parse(platform); err != nil || (u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		return fmt.Errorf("platform URL must be https (got %q)", platform)
	}
	name := runnerName
	if name == "" {
		name, _ = os.Hostname()
	}
	if runnerPublicURL != "" {
		if u, err := url.Parse(runnerPublicURL); err != nil || u.Scheme != "https" {
			return fmt.Errorf("--public-url must be an https URL (got %q)", runnerPublicURL)
		}
	}
	localDirs, err := resolveWorkspaceDirs(runnerWorkspaces, st.LocalDirs)
	if err != nil {
		return err
	}
	if err := applyImageFlag(st); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := preflightLinkImage(ctx, cmd, st, ""); err != nil {
		return err
	}
	client := runner.NewClient(platform, runner.Credentials{})
	start, err := client.StartLink(ctx, name, runtime.GOOS, runtime.GOARCH, st.Mode)
	if err != nil {
		return err
	}
	verify := firstNonEmpty(start.VerificationURIComplete, start.VerificationURI)
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "To link %q, approve it on the platform:\n\n  %s\n\nand check the code matches: %s\n\nWaiting for approval…\n", name, verify, start.UserCode)
	if !runnerNoBrowser {
		runner.OpenURL(verify)
	}

	creds, err := pollLink(ctx, client, start)
	if err != nil {
		return err
	}
	keyStore, err := runner.SaveAPIKey(creds.APIKey)
	if err != nil {
		return fmt.Errorf("storing the runner key failed (%w); remove %q from the platform's Runners page and link again", err, name)
	}
	st.PlatformURL = strings.TrimRight(platform, "/")
	st.RunnerID = creds.RunnerID
	st.Name = name
	if runnerPublicURL != "" {
		st.PublicURL = runnerPublicURL
	}
	if runnerListen != "" {
		st.ListenAddr = runnerListen
	}
	st.LocalDirs = localDirs
	if err := st.Save(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Linked as %q (mode: %s; key stored in the %s). Start it with `sprout runner start`, or `sprout runner install` to run at login.\n", name, st.Mode, keyStore)
	return printLocalDirNotice(cmd, st)
}

// applyImageFlag stores --image in the state. A later `link` or `start`
// without the flag keeps the saved image.
func applyImageFlag(st *runner.State) error {
	image := strings.TrimSpace(runnerImage)
	if image == "" {
		return nil
	}
	if err := runner.ValidateImage(image); err != nil {
		return fmt.Errorf("--image: %w", err)
	}
	st.Image = image
	return nil
}

// preflightLinkImage refuses to link a container-mode runner whose image
// cannot be pulled, before the user approves anything on the platform. With
// no Docker engine running it only warns: `runner start` checks again, and
// the user may start Docker or switch modes in between.
func preflightLinkImage(ctx context.Context, cmd *cobra.Command, st *runner.State, docker string) error {
	if st.Mode != runner.ModeContainer {
		return nil
	}
	if err := runner.DockerAvailable(ctx, docker); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n`sprout runner start` needs it running, with the workspace image %s available.\n", err, st.Image)
		return nil
	}
	l := &runner.ContainerLauncher{Image: st.Image, Docker: docker}
	return l.EnsureImage(ctx, cmd.OutOrStdout())
}

// resolveWorkspaceDirs applies the --dir flags to the saved allowlist.
// Passing no flags keeps the saved list, so a plain `runner start` never
// loses the configuration. With flags, the saved list is REPLACED (the flags
// are the whole new list): that is how entries are removed. Re-validate every
// entry so a since-deleted directory fails with a clear error instead of
// silently dropping from the list.
func resolveWorkspaceDirs(flags []string, saved []string) ([]string, error) {
	if len(flags) == 0 {
		return saved, nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(flags))
	for _, raw := range flags {
		dir, err := runner.LocalDir(raw)
		if err != nil {
			return nil, fmt.Errorf("--dir: %w", err)
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	return out, nil
}

// printLocalDirNotice says what a local-directory allowlist means, once, in
// the command's output.
func printLocalDirNotice(cmd *cobra.Command, st *runner.State) error {
	if len(st.LocalDirs) == 0 {
		return nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Local workspace directories (work started there uses your real files):\n")
	for _, dir := range st.LocalDirs {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", dir)
	}
	return nil
}

func pollLink(ctx context.Context, client *runner.Client, start *runner.DeviceStart) (*runner.Credentials, error) {
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

func runRunnerStart(cmd *cobra.Command, _ []string) error {
	st, err := runner.LoadState()
	if err != nil {
		return err
	}
	if !st.Linked() {
		return errors.New("this machine is not linked; run `sprout runner link` first")
	}
	key, err := runner.LoadAPIKey()
	if err != nil || key == "" {
		return errors.New("the runner key is missing from the credential store; run `sprout runner link` again")
	}
	st.LocalDirs, err = resolveWorkspaceDirs(runnerStartLocal, st.LocalDirs)
	if err != nil {
		return err
	}
	if err := applyImageFlag(st); err != nil {
		return err
	}
	if len(runnerStartLocal) > 0 || runnerImage != "" {
		if err := st.Save(); err != nil {
			return err
		}
	}
	if len(runnerStartLocal) > 0 {
		if err := printLocalDirNotice(cmd, st); err != nil {
			return err
		}
	}
	launcher, capability, err := launcherFor(cmd.Context(), cmd.OutOrStdout(), st, "")
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := &runner.Runner{
		State:    st,
		Client:   runner.NewClient(st.PlatformURL, runner.Credentials{RunnerID: st.RunnerID, APIKey: key}),
		Launcher: launcher,
		Host:     runner.NewHostServer(),
		Sandbox:  capability,
		Version:  buildinfo.Version,
	}
	return r.Run(ctx)
}

// launcherFor picks the launcher for the configured mode and the sandbox name
// reported to the platform. Native mode refuses to start unsandboxed, and
// container mode refuses to start without its image. docker overrides the
// docker CLI ("" means docker on PATH).
func launcherFor(ctx context.Context, out io.Writer, st *runner.State, docker string) (runner.Launcher, string, error) {
	switch st.Mode {
	case runner.ModeContainer:
		if err := runner.DockerAvailable(ctx, docker); err != nil {
			return nil, "", err
		}
		l := &runner.ContainerLauncher{Image: st.Image, Docker: docker}
		if err := l.EnsureImage(ctx, out); err != nil {
			return nil, "", err
		}
		return l, "docker", nil
	case runner.ModeNative:
		c := sandbox.Detect()
		if !c.Available {
			return nil, "", fmt.Errorf("native mode needs the OS sandbox, which is unavailable here: %s", c.Detail)
		}
		return &runner.HostLauncher{Sandboxed: true, Writable: st.Writable}, c.Name, nil
	case runner.ModeBareMetal:
		return &runner.HostLauncher{}, "none", nil
	}
	return nil, "", fmt.Errorf("unknown mode %q", st.Mode)
}

func runRunnerStatus(cmd *cobra.Command, _ []string) error {
	st, err := runner.LoadState()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if !st.Linked() {
		_, _ = fmt.Fprintln(out, "Not linked. Run `sprout runner link --platform https://…`.")
		return nil
	}
	c := sandbox.Detect()
	_, _ = fmt.Fprintf(out, "Linked:    %s (%s)\nPlatform:  %s\nMode:      %s\n", st.Name, st.RunnerID, st.PlatformURL, st.Mode)
	_, _ = fmt.Fprintf(out, "Sandbox:   %s", c.Name)
	switch {
	case !c.Available:
		_, _ = fmt.Fprintf(out, " (unavailable: %s)", c.Detail)
	case c.Weak:
		_, _ = fmt.Fprint(out, " (weak isolation)")
	}
	_, _ = fmt.Fprintf(out, "\nListen:    %s\nPublic URL: %s\n", st.ListenAddr, firstNonEmpty(st.PublicURL, "(none — connects through the platform relay)"))
	if len(st.LocalDirs) > 0 {
		_, _ = fmt.Fprint(out, "Local workspaces (your real files are the workspace):\n")
		for _, dir := range st.LocalDirs {
			_, _ = fmt.Fprintf(out, "  %s\n", dir)
		}
	}
	if st.Mode == runner.ModeContainer {
		_, _ = fmt.Fprintf(out, "Image:     %s\n", st.Image)
		if err := runner.DockerAvailable(cmd.Context(), ""); err != nil {
			_, _ = fmt.Fprintf(out, "Container: %v\n", err)
		}
	}
	return nil
}

func runRunnerMode(cmd *cobra.Command, args []string) error {
	mode := args[0]
	if !runner.ValidMode(mode) {
		return fmt.Errorf("unknown mode %q (container, native, bare-metal)", mode)
	}
	st, err := runner.LoadState()
	if err != nil {
		return err
	}
	if mode == runner.ModeNative {
		if c := sandbox.Detect(); !c.Available {
			return fmt.Errorf("native mode needs the OS sandbox, which is unavailable here: %s", c.Detail)
		}
	}
	if mode == runner.ModeBareMetal && st.Mode != runner.ModeBareMetal {
		if err := confirmBareMetal(cmd); err != nil {
			return err
		}
	}
	st.Mode = mode
	if err := st.Save(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Mode set to %s. Restart the runner for it to take effect.\n", mode)
	return nil
}

// confirmBareMetal requires an interactive confirmation on this machine: the
// platform, a script or an agent must never be able to switch it on.
func confirmBareMetal(cmd *cobra.Command) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("bare-metal mode must be confirmed interactively on this machine")
	}
	_, _ = fmt.Fprint(cmd.OutOrStdout(), `Bare-metal mode runs workspace commands with no sandbox: anything the
browser build escalates can read and change everything your user can,
including SSH keys, cloud credentials and other projects.

Type "bare metal" to confirm: `)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(strings.ToLower(line)) != "bare metal" {
		return errors.New("not confirmed; mode unchanged")
	}
	return nil
}

// unlinkMessage explains that unlinking is local only: the runner protocol has
// no runner-initiated revoke, so the key stays valid until the backend that
// issued it revokes it.
const unlinkMessage = `Unlinked: this machine's runner key and link are removed locally.
The key stays valid on the backend until you revoke it there: on the hosted
platform, remove the runner on its Runners page; a self-hosted backend
provides its own revoke route (the runner protocol has no runner-side revoke).`

func runRunnerUnlink(cmd *cobra.Command, _ []string) error {
	st, err := runner.LoadState()
	if err != nil {
		return err
	}
	_ = runner.DeleteAPIKey()
	st.RunnerID, st.Name = "", ""
	if err := st.Save(); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), unlinkMessage)
	return nil
}

func runRunnerInstall(cmd *cobra.Command, _ []string) error {
	st, err := runner.LoadState()
	if err != nil {
		return err
	}
	if !st.Linked() {
		return errors.New("link this machine first: `sprout runner link`")
	}
	path, err := runner.InstallService()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Installed %s; the runner starts at login.\n", path)
	return nil
}

func runRunnerUninstall(cmd *cobra.Command, _ []string) error {
	if err := runner.UninstallService(); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Removed the runner's login service.")
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
