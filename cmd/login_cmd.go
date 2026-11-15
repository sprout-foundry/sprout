package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/platformauth"
	"github.com/sprout-foundry/sprout/pkg/runner"
)

var (
	loginPlatform  string
	loginNoBrowser bool
)

func init() {
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Sign this machine's sprout in to your platform account",
		Long: `Sign sprout in to your platform account: approve it once in the browser,
and ` + "`sprout runner link`" + ` and ` + "`sprout gpu link`" + ` then link this machine to that
account without another approval. On Pro and Team plans it also adds the
platform's MCP server to sprout ("sprout-foundry"), so sprout can list your
projects, read task results and add, run and manage tasks for you. It has no
direct git access, but the tasks it adds change your repositories as any task
does and spend your credits, and sprout asks before each platform tool call
unless you allow it. Signing in again replaces (and revokes) the previous
sign-in; ` + "`sprout logout`" + ` revokes it, as does removing it under
Account → Developer on the platform.`,
		Args: cobra.NoArgs,
		RunE: runLogin,
	}
	loginCmd.Flags().StringVar(&loginPlatform, "platform", "", "platform URL (default $SPROUT_PLATFORM_URL, else the previous sign-in, else "+runner.DefaultPlatformURL+")")
	loginCmd.Flags().BoolVar(&loginNoBrowser, "no-browser", false, "print the approval URL without opening a browser (SSH sessions, headless machines)")
	rootCmd.AddCommand(
		loginCmd,
		&cobra.Command{Use: "logout", Short: "Forget this machine's platform sign-in", Args: cobra.NoArgs, RunE: runLogout},
		&cobra.Command{Use: "whoami", Short: "Show the platform account this machine's sprout is signed in to", Args: cobra.NoArgs, RunE: runWhoami},
	)
}

// linkedTo is " to <email>" for a link message, "" when the account is not
// known.
func linkedTo(email string) string {
	if email == "" {
		return ""
	}
	return " to " + email
}

func runLogin(cmd *cobra.Command, _ []string) error {
	saved, previousToken := "", ""
	if l, token, ok := platformauth.Load(); ok {
		saved, previousToken = l.PlatformURL, token
	}
	platform := resolveLinkPlatform(loginPlatform, os.Getenv("SPROUT_PLATFORM_URL"), saved)
	if u, err := url.Parse(platform); err != nil || (u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		return fmt.Errorf("platform URL must be https (got %q)", platform)
	}
	name, _ := os.Hostname()
	out := cmd.OutOrStdout()
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := platformauth.NewClient(platform, "")
	start, err := client.StartLogin(ctx, name, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	verify := start.VerificationURI
	_, _ = fmt.Fprintf(out, "To sign in, open\n\n  %s\n\nand enter this code: %s\n\nWaiting for approval…\n", verify, start.UserCode)
	if !loginNoBrowser {
		runner.OpenURL(verify)
	}
	granted, err := pollLogin(ctx, client, start)
	if err != nil {
		return err
	}
	store, err := platformauth.Save(&platformauth.Login{PlatformURL: platform, Email: granted.AccountEmail}, granted.AccessToken)
	if err != nil {
		return err
	}
	if previousToken != "" && previousToken != granted.AccessToken {
		// The new sign-in replaces the old one: revoke it, so it does not
		// stay valid on the platform after this machine forgot it.
		if err := platformauth.NewClient(saved, previousToken).Logout(ctx); err != nil {
			_, _ = fmt.Fprintf(out, "Could not revoke the previous sign-in (%v); remove it under Account → Developer.\n", err)
		}
	}
	as := ""
	if granted.AccountEmail != "" {
		as = " as " + granted.AccountEmail
	}
	_, _ = fmt.Fprintf(out, "Signed in to %s%s (kept in the %s). `sprout runner link` and `sprout gpu link` now link this machine to this account.\n", platform, as, store)
	connectPlatformMCP(ctx, out, platform, granted.AccessToken)
	return nil
}

// connectPlatformMCP adds the platform's MCP server to sprout's MCP config
// when the account's plan includes it, so sprout's agent can work with the
// person's projects and tasks. A failure here leaves the sign-in in place.
func connectPlatformMCP(ctx context.Context, out io.Writer, platform, token string) {
	account, err := platformauth.NewClient(platform, token).WhoAmI(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(out, "Could not check whether your plan includes the platform's MCP server (%v); run `sprout login` again to connect it.\n", err)
		return
	}
	if !account.MCPAllowed {
		if removed, _ := platformauth.RemoveMCP(); removed {
			_, _ = fmt.Fprintf(out, "Removed the %q MCP server: your %s plan does not include it.\n", platformauth.MCPServerName, account.Plan)
			return
		}
		_, _ = fmt.Fprintf(out, "Your %s plan does not include the platform's MCP server; on Pro and Team, sprout can also work with your projects and tasks there.\n", account.Plan)
		return
	}
	if err := platformauth.ConfigureMCP(platform, token); err != nil {
		_, _ = fmt.Fprintf(out, "Could not add the platform's MCP server (%v); run `sprout login` again to retry.\n", err)
		return
	}
	_, _ = fmt.Fprintf(out, "Added the %q MCP server (%s): sprout can now list your projects and add, run and manage their tasks for you (tasks change your repositories and spend credits; sprout asks before each platform tool call unless you allow it). It starts with your next sprout session.\n", platformauth.MCPServerName, platformauth.MCPURL(platform))
}

func pollLogin(ctx context.Context, client *platformauth.Client, start *runner.DeviceStart) (*platformauth.Granted, error) {
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
		granted, err := client.PollLogin(ctx, start.DeviceCode)
		switch {
		case err == nil:
			return granted, nil
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

func runLogout(cmd *cobra.Command, _ []string) error {
	l, token, ok := platformauth.Load()
	revoked := false
	if ok {
		if err := platformauth.NewClient(l.PlatformURL, token).Logout(cmd.Context()); err != nil {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Could not revoke the sign-in on the platform (%v); remove it under Account → Developer.\n", err)
		} else {
			revoked = true
		}
	}
	if err := platformauth.Remove(); err != nil {
		return err
	}
	removedMCP, mcpErr := platformauth.RemoveMCP()
	if mcpErr != nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Could not remove the %q MCP server: %v\n", platformauth.MCPServerName, mcpErr)
	} else if removedMCP {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed the %q MCP server.\n", platformauth.MCPServerName)
	}
	if !ok {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Not signed in.")
		return nil
	}
	if revoked {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Signed out of %s and revoked the sign-in on the platform.\n", l.PlatformURL)
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Signed out of %s on this machine.\n", l.PlatformURL)
	}
	return nil
}

func runWhoami(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	l, token, ok := platformauth.Load()
	if !ok {
		_, _ = fmt.Fprintln(out, "Not signed in. Run `sprout login`.")
		return nil
	}
	account, err := platformauth.NewClient(l.PlatformURL, token).WhoAmI(cmd.Context())
	if errors.Is(err, platformauth.ErrNotSignedIn) {
		return fmt.Errorf("the platform no longer accepts this sign-in (it was revoked); run `sprout login` again")
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Signed in to %s as %s (%s plan)\n", l.PlatformURL, account.Email, account.Plan)
	if account.MCPAllowed {
		_, _ = fmt.Fprintf(out, "MCP server: %s (%q in sprout's MCP config)\n", platformauth.MCPURL(l.PlatformURL), platformauth.MCPServerName)
	}
	return nil
}
