package utils

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
)

// SecurityPromptHook, when non-nil, replaces the line-based key entry in
// AskForApprovalWithOptions with an interactive picker (arrow-key SelectList
// in pkg/console).  Registered at pkg/console init() time.  Leaving the hook
// nil keeps the legacy "[y/n/a/e]" path so this package stays leaf-level —
// no upward dependency on pkg/console.
var SecurityPromptHook func(prompt, command string) ApprovalChoice

// FilesystemSecurityPromptHook is the matching hook for AskForFilesystemApproval.
// Same registration pattern as SecurityPromptHook.
var FilesystemSecurityPromptHook func(prompt, path, folder string, tier FilesystemPromptTier) ApprovalChoice

// ApprovalChoice is the typed result of AskForApprovalWithOptions — the
// 4-option CLI prompt that lets the user respond to a security gate with
// Deny / Approve once / Always approve / Elevate.
//
// Defined here (not in pkg/security) so pkg/utils can stay leaf-level —
// the agent layer maps this to security.ApprovalDecision at the callsite.
type ApprovalChoice int

const (
	// ApprovalChoiceDeny rejects the operation.
	ApprovalChoiceDeny ApprovalChoice = iota
	// ApprovalChoiceApproveOnce allows this single invocation.
	ApprovalChoiceApproveOnce
	// ApprovalChoiceApproveAlways allows this invocation and persists
	// the command to the user's allowlist (Config.ApprovedShellCommands).
	ApprovalChoiceApproveAlways
	// ApprovalChoiceElevate allows this invocation and sets the session
	// risk-profile override to permissive.
	ApprovalChoiceElevate
	// ApprovalChoiceAllowFolderSession allows this invocation and adds
	// the prompt's target folder to the agent's session-allowed list,
	// auto-approving future accesses under that folder. Only offered
	// for the External filesystem tier.
	ApprovalChoiceAllowFolderSession
)

// FilesystemPromptTier picks the option set for the filesystem
// approval prompt. PathTierExternal gets 3 options (Allow once /
// Allow folder this session / Deny); PathTierSensitive gets 2
// (Allow once / Deny) — sensitive paths can never be session-
// allowlisted because they're system or off-CWD home files.
type FilesystemPromptTier int

const (
	// FilesystemPromptExternal — Tier B, 3 options including
	// "Allow this folder for the rest of the session".
	FilesystemPromptExternal FilesystemPromptTier = iota
	// FilesystemPromptSensitive — Tier C, 2 options. The "Allow
	// folder this session" choice is suppressed.
	FilesystemPromptSensitive
)

// AskForApprovalWithOptions prompts the user with a 4-option menu for a
// high-risk shell command. Returns the chosen ApprovalChoice. On stdin
// unavailable / non-interactive, returns ApprovalChoiceDeny for safety.
//
// The prompt renders the command on its own line so the user can see what
// they're approving, then lists the four options with single-letter keys.
// The Elevate option carries an inline disclaimer so users understand
// they're loosening the gate for the rest of the session, not forever.
func (w *Logger) AskForApprovalWithOptions(prompt, command string) ApprovalChoice {
	loggerMu.RLock()
	interactive := w.userInteractionEnabled
	loggerMu.RUnlock()
	if !interactive {
		w.Log("Skipping user confirmation in non-interactive mode — denying for safety.")
		return ApprovalChoiceDeny
	}

	// If pkg/console has registered an arrow-key picker, prefer it for
	// visual consistency with the rest of the CLI (model picker, session
	// picker, etc.).  The hook itself handles clihooks / TTY suspension
	// and its own non-TTY fallback, so we bypass the legacy code path
	// entirely when it's available.
	if SecurityPromptHook != nil {
		return SecurityPromptHook(prompt, command)
	}

	clihooks.SuspendIndicator()
	clihooks.PauseSteer()
	defer clihooks.ResumeSteer()

	reader := bufio.NewReader(os.Stdin)
	consecutiveErrors := 0
	const maxConsecutiveErrors = 3

	menu := strings.Join([]string{
		"  [y] Approve once         — allow this invocation only",
		"  [n] Deny                 — reject and surface a security error",
		"  [a] Always approve       — persist this exact command to your allowlist",
		"  [e] Elevate (session)    — bump to 'permissive' for the rest of this session",
		"                            (Critical ops still block. Run /risk-profile permissive",
		"                             to make this persistent across restarts.)",
	}, "\n")

	// Print the full prompt + menu once, then loop only the short
	// "Choose ..." line on invalid input. Re-printing the entire block
	// per typo would flood the terminal.
	w.LogUserInteraction(fmt.Sprintf("%s\nCommand:\n  %s\n\n%s\n", prompt, command, menu))

	for {
		w.LogUserInteraction("Choose [y/n/a/e]: ")
		response, err := reader.ReadString('\n')
		if err != nil {
			consecutiveErrors++
			w.Log(fmt.Sprintf("AskForApprovalWithOptions: read error (attempt %d/%d): %v", consecutiveErrors, maxConsecutiveErrors, err))
			if consecutiveErrors >= maxConsecutiveErrors {
				w.LogUserInteraction(" stdin unavailable - denying for safety.")
				return ApprovalChoiceDeny
			}
			continue
		}
		consecutiveErrors = 0

		switch strings.ToLower(strings.TrimSpace(response)) {
		case "y", "yes":
			return ApprovalChoiceApproveOnce
		case "n", "no":
			return ApprovalChoiceDeny
		case "a", "always":
			return ApprovalChoiceApproveAlways
		case "e", "elevate":
			return ApprovalChoiceElevate
		default:
			w.LogUserInteraction("Invalid input. Type one of: y / n / a / e.")
		}
	}
}

// AskForFilesystemApproval prompts the user about an out-of-workspace
// filesystem access. The option set depends on tier:
//
//   - FilesystemPromptExternal: 3 options — Allow once / Allow this
//     folder for the rest of the session / Deny. Picking the folder
//     option causes the agent to persist `folder` to its session
//     allowlist so future paths under it auto-approve.
//
//   - FilesystemPromptSensitive: 2 options — Allow once / Deny.
//     System paths and off-CWD home paths cannot be session-allow-
//     listed; the dialog calls this out so the user understands why
//     they'll keep seeing the prompt.
//
// On stdin unavailable / non-interactive, returns ApprovalChoiceDeny.
// `path` is the file being accessed; `folder` is the directory the
// agent would add to the allowlist if the user picks the folder
// option (typically the parent dir of `path`).
func (w *Logger) AskForFilesystemApproval(prompt, path, folder string, tier FilesystemPromptTier) ApprovalChoice {
	loggerMu.RLock()
	interactive := w.userInteractionEnabled
	loggerMu.RUnlock()
	if !interactive {
		w.Log("Skipping filesystem approval in non-interactive mode — denying for safety.")
		return ApprovalChoiceDeny
	}

	// Prefer the arrow-key picker (registered by pkg/console at init) when
	// available, same as AskForApprovalWithOptions above.
	if FilesystemSecurityPromptHook != nil {
		return FilesystemSecurityPromptHook(prompt, path, folder, tier)
	}

	clihooks.SuspendIndicator()
	clihooks.PauseSteer()
	defer clihooks.ResumeSteer()

	reader := bufio.NewReader(os.Stdin)
	consecutiveErrors := 0
	const maxConsecutiveErrors = 3

	var menu, choiceHint string
	switch tier {
	case FilesystemPromptSensitive:
		menu = strings.Join([]string{
			"  [y] Allow once         — read/write this path this time only",
			"  [n] Deny               — reject and surface a security error",
			"",
			"  This path is in a SENSITIVE location (system dir, or home",
			"  while CWD is outside home). It can't be added to the session",
			"  allowlist; every access will prompt.",
		}, "\n")
		choiceHint = "Choose [y/n]: "
	default: // FilesystemPromptExternal
		menu = strings.Join([]string{
			"  [y] Allow once               — read/write this path this time only",
			"  [n] Deny                     — reject and surface a security error",
			fmt.Sprintf("  [f] Allow folder this session — auto-approve everything under\n        %s\n        for the rest of this session", folder),
		}, "\n")
		choiceHint = "Choose [y/n/f]: "
	}

	w.LogUserInteraction(fmt.Sprintf("%s\nPath:\n  %s\n\n%s\n", prompt, path, menu))

	for {
		w.LogUserInteraction(choiceHint)
		response, err := reader.ReadString('\n')
		if err != nil {
			consecutiveErrors++
			w.Log(fmt.Sprintf("AskForFilesystemApproval: read error (attempt %d/%d): %v", consecutiveErrors, maxConsecutiveErrors, err))
			if consecutiveErrors >= maxConsecutiveErrors {
				w.LogUserInteraction(" stdin unavailable - denying for safety.")
				return ApprovalChoiceDeny
			}
			continue
		}
		consecutiveErrors = 0

		switch strings.ToLower(strings.TrimSpace(response)) {
		case "y", "yes":
			return ApprovalChoiceApproveOnce
		case "n", "no":
			return ApprovalChoiceDeny
		case "f", "folder":
			if tier == FilesystemPromptSensitive {
				w.LogUserInteraction("This path can't be allowlisted (Sensitive tier). Pick y or n.")
				continue
			}
			return ApprovalChoiceAllowFolderSession
		default:
			if tier == FilesystemPromptSensitive {
				w.LogUserInteraction("Invalid input. Type one of: y / n.")
			} else {
				w.LogUserInteraction("Invalid input. Type one of: y / n / f.")
			}
		}
	}
}
