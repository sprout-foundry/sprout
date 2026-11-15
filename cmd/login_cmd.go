package cmd

import (
	"context"
	"errors"
	"fmt"
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
account without another approval. The sign-in can see who you are and link
machines to your account, and nothing else; revoke it under Account →
Developer on the platform, or with ` + "`sprout logout`" + ` here.`,
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
	saved := ""
	if l, _, ok := platformauth.Load(); ok {
		saved = l.PlatformURL
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
	verify := firstNonEmpty(start.VerificationURIComplete, start.VerificationURI)
	_, _ = fmt.Fprintf(out, "To sign in, approve it on the platform:\n\n  %s\n\nand check the code matches: %s\n\nWaiting for approval…\n", verify, start.UserCode)
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
	as := ""
	if granted.AccountEmail != "" {
		as = " as " + granted.AccountEmail
	}
	_, _ = fmt.Fprintf(out, "Signed in to %s%s (kept in the %s). `sprout runner link` and `sprout gpu link` now link this machine to this account.\n", platform, as, store)
	return nil
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
	l, _, ok := platformauth.Load()
	if err := platformauth.Remove(); err != nil {
		return err
	}
	if !ok {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Not signed in.")
		return nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Signed out of %s on this machine. To revoke the sign-in everywhere, remove its token under Account → Developer.\n", l.PlatformURL)
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
	return nil
}
