package utils

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
)

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

	for {
		w.LogUserInteraction(fmt.Sprintf("%s\nCommand:\n  %s\n\n%s\n\nChoose [y/n/a/e]: ", prompt, command, menu))
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
