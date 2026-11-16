// flow.go — the sign-in flow itself, shared by `sprout login` and the
// `/login` slash command. It writes to an io.Writer and takes a context so a
// terminal, a WebUI response and a test can each drive it; the CLI and the
// agent each keep only the part that is theirs (flags, argument parsing).
package platformauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/sprout-foundry/sprout/pkg/runner"
)

// LoginOptions carries the choices a caller makes before signing in.
type LoginOptions struct {
	// PlatformURL is the explicit choice (--platform / the /login argument).
	// Empty falls back to $SPROUT_PLATFORM_URL, then the previous sign-in,
	// then runner.DefaultPlatformURL.
	PlatformURL string
	// NoBrowser prints the approval URL instead of opening a browser:
	// SSH sessions, headless machines, and any surface with no browser to
	// open (the WebUI).
	NoBrowser bool
	// OpenURL opens the approval URL; nil uses runner.OpenURL. A slash
	// command on a machine with no browser, or a test, replaces it.
	OpenURL func(string)
	// MachineName, when set, names this machine on the platform instead of
	// the hostname.
	MachineName string
}

// Flow runs sign-in, sign-out and status for one surface.
type Flow struct {
	Out io.Writer
}

// NewFlow returns a flow that reports to out.
func NewFlow(out io.Writer) *Flow {
	if out == nil {
		out = io.Discard
	}
	return &Flow{Out: out}
}

// ResolvePlatform picks the platform URL from an explicit choice,
// $SPROUT_PLATFORM_URL, the previous sign-in, and the default, in that order.
func ResolvePlatform(explicit string) string {
	for _, candidate := range []string{explicit, os.Getenv("SPROUT_PLATFORM_URL"), savedPlatformURL(), runner.DefaultPlatformURL} {
		if candidate != "" {
			return candidate
		}
	}
	return runner.DefaultPlatformURL
}

func savedPlatformURL() string {
	l, _, ok := Load()
	if !ok {
		return ""
	}
	return l.PlatformURL
}

// LinkedTo is " to <email>" for a link message, "" when the account is not
// known.
func LinkedTo(email string) string {
	if email == "" {
		return ""
	}
	return " to " + email
}

// validatePlatform accepts https only, with localhost as the development
// exception, so a sign-in token never travels over plaintext.
func validatePlatform(platform string) error {
	u, err := url.Parse(platform)
	if err != nil || (u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		return fmt.Errorf("platform URL must be https (got %q)", platform)
	}
	return nil
}

// Login runs the device-code sign-in: print the approval URL and code, wait
// for the approval, store the token, and connect the platform's MCP server
// when the plan includes it. A second sign-in replaces the previous one.
func (f *Flow) Login(ctx context.Context, opts LoginOptions) error {
	platform := ResolvePlatform(opts.PlatformURL)
	if err := validatePlatform(platform); err != nil {
		return err
	}
	name := opts.MachineName
	if name == "" {
		name, _ = os.Hostname()
	}
	open := opts.OpenURL
	if open == nil {
		open = runner.OpenURL
	}

	saved, previousToken := "", ""
	if l, token, ok := Load(); ok {
		saved, previousToken = l.PlatformURL, token
	}

	client := NewClient(platform, "")
	start, err := client.StartLogin(ctx, name, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(f.Out, "To sign in, open\n\n  %s\n\nand enter this code: %s\n\nWaiting for approval", start.VerificationURI, start.UserCode)
	if start.ExpiresIn > 0 {
		_, _ = fmt.Fprintf(f.Out, " (the code expires in %s)", time.Duration(start.ExpiresIn)*time.Second)
	}
	_, _ = fmt.Fprintln(f.Out, "…")
	if !opts.NoBrowser {
		open(start.VerificationURI)
	}
	granted, err := f.poll(ctx, client, start)
	if err != nil {
		return err
	}
	store, err := Save(&Login{PlatformURL: platform, Email: granted.AccountEmail}, granted.AccessToken)
	if err != nil {
		return err
	}
	if previousToken != "" && previousToken != granted.AccessToken {
		// The new sign-in replaces the old one: revoke it, so it does not
		// stay valid on the platform after this machine forgot it.
		if err := NewClient(saved, previousToken).Logout(ctx); err != nil {
			_, _ = fmt.Fprintf(f.Out, "Could not revoke the previous sign-in (%v); remove it under Account → Developer.\n", err)
		}
	}
	as := ""
	if granted.AccountEmail != "" {
		as = " as " + granted.AccountEmail
	}
	_, _ = fmt.Fprintf(f.Out, "Signed in to %s%s (kept in the %s). `sprout runner link` and `sprout gpu link` now link this machine to this account.\n", platform, as, store)
	f.connectMCP(ctx, platform, granted.AccessToken)
	return nil
}

// minPollInterval is the wait between approval checks when the platform
// does not say how often to check. Tests shorten it to keep the suite quick.
var minPollInterval = 5 * time.Second

// poll waits for the approval, honouring the platform's interval and its
// slow-down, until the code expires or ctx ends.
func (f *Flow) poll(ctx context.Context, client *Client, start *runner.DeviceStart) (*Granted, error) {
	interval := time.Duration(start.Interval) * time.Second
	if interval <= 0 {
		interval = minPollInterval
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
			interval += minPollInterval
		default:
			return nil, err
		}
		if start.ExpiresIn > 0 && time.Now().After(deadline) {
			return nil, runner.ErrExpiredToken
		}
	}
}

// connectMCP adds the platform's MCP server when the account's plan includes
// it, so sprout's agent can work with the person's projects and tasks. A
// failure here leaves the sign-in in place.
func (f *Flow) connectMCP(ctx context.Context, platform, token string) {
	account, err := NewClient(platform, token).WhoAmI(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(f.Out, "Could not check whether your plan includes the platform's MCP server (%v); run `sprout login` again to connect it.\n", err)
		return
	}
	if !account.MCPAllowed {
		if removed, _ := RemoveMCP(); removed {
			_, _ = fmt.Fprintf(f.Out, "Removed the %q MCP server: your %s plan does not include it.\n", MCPServerName, account.Plan)
			return
		}
		_, _ = fmt.Fprintf(f.Out, "Your %s plan does not include the platform's MCP server; on Pro and Team, sprout can also work with your projects and tasks there.\n", account.Plan)
		return
	}
	if err := ConfigureMCP(platform, token); err != nil {
		_, _ = fmt.Fprintf(f.Out, "Could not add the platform's MCP server (%v); run `sprout login` again to retry.\n", err)
		return
	}
	_, _ = fmt.Fprintf(f.Out, "Added the %q MCP server (%s): sprout can now list your projects and add, run and manage their tasks for you (tasks change your repositories and spend credits; sprout asks before each platform tool call unless you allow it). It starts with your next sprout session.\n", MCPServerName, MCPURL(platform))
}

// SignOut forgets this machine's sign-in and revokes it on the platform.
func (f *Flow) SignOut(ctx context.Context) error {
	l, token, ok := Load()
	revoked := false
	if ok {
		if err := NewClient(l.PlatformURL, token).Logout(ctx); err != nil {
			_, _ = fmt.Fprintf(f.Out, "Could not revoke the sign-in on the platform (%v); remove it under Account → Developer.\n", err)
		} else {
			revoked = true
		}
	}
	if err := Remove(); err != nil {
		return err
	}
	removedMCP, mcpErr := RemoveMCP()
	if mcpErr != nil {
		_, _ = fmt.Fprintf(f.Out, "Could not remove the %q MCP server: %v\n", MCPServerName, mcpErr)
	} else if removedMCP {
		_, _ = fmt.Fprintf(f.Out, "Removed the %q MCP server.\n", MCPServerName)
	}
	if !ok {
		_, _ = fmt.Fprintln(f.Out, "Not signed in.")
		return nil
	}
	switch {
	case revoked:
		_, _ = fmt.Fprintf(f.Out, "Signed out of %s and revoked the sign-in on the platform.\n", l.PlatformURL)
	default:
		_, _ = fmt.Fprintf(f.Out, "Signed out of %s on this machine.\n", l.PlatformURL)
	}
	return nil
}

// Status reports the account this machine is signed in to.
func (f *Flow) Status(ctx context.Context) error {
	l, token, ok := Load()
	if !ok {
		_, _ = fmt.Fprintln(f.Out, "Not signed in. Run `sprout login` or /login.")
		return nil
	}
	account, err := NewClient(l.PlatformURL, token).WhoAmI(ctx)
	if errors.Is(err, ErrNotSignedIn) {
		return fmt.Errorf("the platform no longer accepts this sign-in (it was revoked); run `sprout login` again")
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(f.Out, "Signed in to %s as %s (%s plan)\n", l.PlatformURL, account.Email, account.Plan)
	if account.MCPAllowed {
		_, _ = fmt.Fprintf(f.Out, "MCP server: %s (%q in sprout's MCP config)\n", MCPURL(l.PlatformURL), MCPServerName)
	}
	return nil
}
