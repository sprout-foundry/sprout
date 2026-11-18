package commands

// login.go — the /login, /logout and /whoami slash commands: the same
// platform sign-in as `sprout login`, available without leaving a session.
// The flow itself lives in pkg/platformauth, shared with the CLI, so the
// approval URL, the token storage and the MCP hand-off exist in one place.

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/platformauth"
)

// LoginCommand implements the /login slash command.
type LoginCommand struct {
	outputSink
}

// Name returns the command name.
func (l *LoginCommand) Name() string { return "login" }

// Description returns the command description.
func (l *LoginCommand) Description() string {
	return "Sign in to your platform account (projects and tasks)"
}

// SafeDuringSteer returns false: the sign-in waits in this call for an
// approval made in a browser, which can take minutes, so it cannot run
// mid-turn. Check the state with /whoami instead.
func (l *LoginCommand) SafeDuringSteer() bool { return false }

// Usage returns the detailed help text shown by `/help login`.
func (l *LoginCommand) Usage() string {
	return strings.Join([]string{
		"/login                 Sign in to your platform account.",
		"/login <platform-url>  Sign in to another platform (https; default $SPROUT_PLATFORM_URL,",
		"                       else the previous sign-in, else the public platform).",
		"/login --no-browser    Print the approval URL without opening a browser (SSH, headless).",
		"",
		"Sprout prints a URL and a code: open the URL, enter the code, and this",
		"command waits for your approval. The session is parked while it waits,",
		"up to the code's expiry; approve in the browser, or leave it and the",
		"wait ends on its own.",
		"",
		"Once signed in, `sprout runner link` and `sprout gpu link` link this",
		"machine to that account without another approval, and on Pro and Team",
		"plans sprout also gets the platform's MCP server, so it can list your",
		"projects and add, run and manage their tasks. Those tasks change your",
		"repositories and spend your credits; sprout asks before each platform",
		"tool call unless you allow it.",
		"",
		"Signing in again replaces (and revokes) the previous sign-in. `/logout`",
		"revokes it, as does removing it under Account → Developer on the platform.",
		"",
		"Signs in this machine, so the terminal and the browser share it. Run it",
		"in the terminal: the approval waits here, not in the chat.",
	}, "\n")
}

// Complete provides argument completions for /login. The completer passes the
// arguments without the command name, the last one the token being typed, and
// filters what is returned by that prefix. /login takes a platform URL and
// --no-browser, so the flag is offered until it has been given.
func (l *LoginCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	seen := args
	if len(seen) > 0 {
		seen = seen[:len(seen)-1]
	}
	for _, given := range seen {
		if given == "--no-browser" || given == "-n" {
			return nil
		}
	}
	return []string{"--no-browser"}
}

// Execute signs this machine in.
func (l *LoginCommand) Execute(args []string, chatAgent *agent.Agent) error {
	opts, err := parseLoginArgs(args)
	if err != nil {
		return err
	}
	if err := platformauth.NewFlow(l.out()).Login(commandContext(chatAgent), opts); err != nil {
		return err
	}
	refreshSessionMCP(l.out(), chatAgent, commandContext(chatAgent),
		"This session's MCP servers were refreshed: the platform's tools (list_projects, add_tasks, wait_for_tasks, …) are available now.")
	return nil
}

// commandContext is the context a sign-in command runs under: the agent's
// interrupt context, the same one /compact and /review take, so an interrupt
// ends the approval wait instead of leaving it running to the code's expiry.
// It heals between turns, so an interrupted sign-in leaves later commands a
// live context.
func commandContext(chatAgent *agent.Agent) context.Context {
	if chatAgent == nil {
		return context.Background()
	}
	return chatAgent.InterruptCtx()
}

// refreshSessionMCP applies a just-completed platform sign-in to the running
// session's MCP servers. The login flow writes the platform's MCP server to
// the workspace's mcp_config.json (or removes it on sign-out), but the
// session's manager was reconciled from the config on disk at startup and has
// no way to know the file changed underneath it. Refreshing re-reads the
// config, starts the newly added server, drops the removed one, and clears
// the tool cache so the next turn sees the new tool set. A failure only costs
// the convenience: the server is already configured on disk and starts with
// the next session, so it is reported, not returned.
func refreshSessionMCP(out io.Writer, chatAgent *agent.Agent, ctx context.Context, note string) {
	if chatAgent == nil {
		return
	}
	if err := chatAgent.RefreshRuntimeConfig(ctx); err != nil {
		fmt.Fprintf(out, "Note: %v (the platform's MCP server will start with your next session).\n", err)
		return
	}
	fmt.Fprintln(out, note)
}

// parseLoginArgs splits a /login invocation into its options.
func parseLoginArgs(args []string) (platformauth.LoginOptions, error) {
	opts := platformauth.LoginOptions{}
	for _, arg := range args {
		switch arg {
		case "--no-browser", "-n":
			opts.NoBrowser = true
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, unknownLoginFlag(arg)
			}
			if opts.PlatformURL != "" {
				return opts, errOnePlatform
			}
			opts.PlatformURL = arg
		}
	}
	return opts, nil
}

func unknownLoginFlag(arg string) error {
	return fmt.Errorf("unknown flag %s. Usage: /login [platform-url] [--no-browser]", arg)
}

var errOnePlatform = fmt.Errorf("expected one platform URL. Usage: /login [platform-url] [--no-browser]")

// LogoutCommand implements the /logout slash command.
type LogoutCommand struct {
	outputSink
}

// Name returns the command name.
func (l *LogoutCommand) Name() string { return "logout" }

// Description returns the command description.
func (l *LogoutCommand) Description() string {
	return "Sign out of the platform account on this machine"
}

// SafeDuringSteer returns false: /logout revokes a credential, so it stays a
// deliberate action rather than a mid-turn steer.
func (l *LogoutCommand) SafeDuringSteer() bool { return false }

// Usage returns the detailed help text shown by `/help logout`.
func (l *LogoutCommand) Usage() string {
	return strings.Join([]string{
		"/logout                Revoke this machine's platform sign-in.",
		"",
		"This revokes the sign-in on the platform, forgets it here, and removes",
		`the platform's MCP server ("sprout-foundry") from sprout's MCP config.`,
		"Your projects, tasks and repositories are untouched. Run `/login` to",
		"sign in again.",
	}, "\n")
}

// Execute revokes and forgets the sign-in.
func (l *LogoutCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) > 0 {
		return fmt.Errorf("logout takes no arguments. Usage: /logout")
	}
	if err := platformauth.NewFlow(l.out()).SignOut(commandContext(chatAgent)); err != nil {
		return err
	}
	refreshSessionMCP(l.out(), chatAgent, commandContext(chatAgent),
		"This session's MCP servers were refreshed: the platform's tools are no longer available.")
	return nil
}

// WhoamiCommand implements the /whoami slash command.
type WhoamiCommand struct {
	outputSink
}

// Name returns the command name.
func (w *WhoamiCommand) Name() string { return "whoami" }

// Description returns the command description.
func (w *WhoamiCommand) Description() string {
	return "Show the platform account this machine is signed in to"
}

// SafeDuringSteer returns true — /whoami only reads.
func (w *WhoamiCommand) SafeDuringSteer() bool { return true }

// Usage returns the detailed help text shown by `/help whoami`.
func (w *WhoamiCommand) Usage() string {
	return strings.Join([]string{
		"/whoami                Show the platform account this machine is signed in to.",
		"",
		"Names the platform, your account and its plan, and the platform's MCP",
		"server when the plan includes it. This is the platform account, not your",
		"login name on this machine. Run `/login` to sign in.",
	}, "\n")
}

// Execute prints the signed-in account.
func (w *WhoamiCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) > 0 {
		return fmt.Errorf("whoami takes no arguments. Usage: /whoami")
	}
	// Not the interrupt context: /whoami is steer-safe, and that context is
	// canceled while a turn is in flight, which would fail a plain status read
	// for no reason. It makes one request and returns.
	return platformauth.NewFlow(w.out()).Status(context.Background())
}
