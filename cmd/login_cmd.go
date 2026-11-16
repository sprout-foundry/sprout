package cmd

import (
	"os"
	"os/signal"
	"syscall"

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
Account → Developer on the platform. The same sign-in is available in a
session as ` + "`/login`" + `.`,
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

// runLogin, runLogout and runWhoami are the CLI surface of the sign-in: they
// turn flags into options and hand the flow to pkg/platformauth, which the
// /login, /logout and /whoami slash commands drive the same way.
func runLogin(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := platformauth.LoginOptions{PlatformURL: loginPlatform, NoBrowser: loginNoBrowser}
	return platformauth.NewFlow(cmd.OutOrStdout()).Login(ctx, opts)
}

func runLogout(cmd *cobra.Command, _ []string) error {
	return platformauth.NewFlow(cmd.OutOrStdout()).SignOut(cmd.Context())
}

func runWhoami(cmd *cobra.Command, _ []string) error {
	return platformauth.NewFlow(cmd.OutOrStdout()).Status(cmd.Context())
}
