package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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
)

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
	linkCmd.Flags().StringSliceVar(&runnerWorkspaces, "dir", nil, "local directory this runner serves workspaces in place (repeatable); the user's real files are the workspace — opt in deliberately")
	linkCmd.Flags().StringSliceVar(&runnerWorkspaces, "workspace", nil, "alias for --dir")
	markAlias(linkCmd.Flags(), "workspace", "dir", aliasDeprecated)

	startCmd := &cobra.Command{Use: "start", Short: "Run the runner in the foreground", Args: cobra.NoArgs, RunE: runRunnerStart}
	startCmd.Flags().StringSliceVar(&runnerStartLocal, "dir", nil, "local directory this runner serves workspaces in place (repeatable; passing it replaces the saved list); the user's real files are the workspace — opt in deliberately")
	startCmd.Flags().StringSliceVar(&runnerStartLocal, "workspace", nil, "alias for --dir")
	markAlias(startCmd.Flags(), "workspace", "dir", aliasDeprecated)

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

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
	if len(runnerStartLocal) > 0 {
		if err := st.Save(); err != nil {
			return err
		}
		if err := printLocalDirNotice(cmd, st); err != nil {
			return err
		}
	}
	launcher, capability, err := launcherFor(cmd.Context(), st)
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
// reported to the platform. Native mode refuses to start unsandboxed.
func launcherFor(ctx context.Context, st *runner.State) (runner.Launcher, string, error) {
	switch st.Mode {
	case runner.ModeContainer:
		if err := runner.DockerAvailable(ctx, ""); err != nil {
			return nil, "", err
		}
		return &runner.ContainerLauncher{Image: st.Image}, "docker", nil
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
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Unlinked. Remove the runner from the platform's Runners page to revoke its key there too.")
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
