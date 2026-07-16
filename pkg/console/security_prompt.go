package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// runApprovalPicker runs sl under a deadline so a security prompt the user
// never answers can't wedge the agent or leave the terminal in raw mode.
// On timeout the SelectList's runTTY loop observes ctx.Done() (its termios
// is set VMIN=0, so the poll loop ticks every few ms) and its deferred
// exitSteerMode restores cooked mode before returning context.DeadlineExceeded.
// Returns ApprovalChoiceDeny on timeout, cancel (Esc/Ctrl-C), or error.
func runApprovalPicker(w io.Writer, sl *SelectList) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), utils.ApprovalPromptTimeout)
	defer cancel()

	value, ok, err := sl.Run(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintf(w, "\n  Approval timed out after %s — denying for safety.\n", utils.ApprovalPromptTimeout)
		return "", false
	}
	if err != nil || !ok {
		return "", false
	}
	return value, true
}

// askForSecurityApproval drives a SelectList-based 4-option approval prompt
// for high-risk shell commands.  Visual treatment puts an amber GlyphWarning
// header and the command on its own block so the security context is obvious
// before the user reaches the picker.
//
// Returns utils.ApprovalChoiceDeny on Esc / Ctrl-C / non-TTY (safe default).
//
// Registered into utils.SecurityPromptHook via init() below so pkg/utils stays
// leaf-level — no upward import on pkg/console.
func askForSecurityApproval(prompt, command string) utils.ApprovalChoice {
	return askForSecurityApprovalWriter(os.Stdout, prompt, command)
}

func askForSecurityApprovalWriter(w io.Writer, prompt, command string) utils.ApprovalChoice {
	clihooks.SuspendIndicator()
	clihooks.PauseSteer()
	clihooks.SuspendStreaming()
	defer clihooks.ResumeSteer()
	defer clihooks.ResumeStreaming()

	// SP-070-2: Ring bell to alert the user that a blocking approval is needed
	fmt.Fprint(w, "\a")

	writeSecurityHeader(w, prompt, "Command", command)
	// Elevate's caveat is rendered above the picker because SelectList items
	// only support single-line label+detail; this preserves the warning from
	// the legacy line-based prompt.
	writeSecurityFootnote(w, "Elevate bumps the session risk-profile to permissive. "+
		"Critical ops still block. Run /risk-profile permissive to make it persistent.")

	items := []SelectItem{
		{Label: "Approve once", Detail: "allow this invocation only", Value: "approve_once"},
		{Label: "Deny", Detail: "reject and surface a security error", Value: "deny"},
		{Label: "Always approve", Detail: "persist this exact command to your allowlist", Value: "approve_always"},
		{Label: "Always ask", Detail: "force a prompt for this command every time", Value: "always_ask"},
		{Label: "Elevate (session)", Detail: "loosen the gate for the rest of this session", Value: "elevate"},
	}

	sl := NewSelectList(SelectListOptions{
		Title:  "High-risk operation — choose an action (Esc denies)",
		Items:  items,
		Footer: "↑/↓ navigate · Enter confirm · Esc denies",
	})

	value, ok := runApprovalPicker(w, sl)
	if !ok {
		return utils.ApprovalChoiceDeny
	}

	switch value {
	case "approve_once":
		return utils.ApprovalChoiceApproveOnce
	case "deny":
		return utils.ApprovalChoiceDeny
	case "approve_always":
		return utils.ApprovalChoiceApproveAlways
	case "always_ask":
		return utils.ApprovalChoiceAlwaysAsk
	case "elevate":
		return utils.ApprovalChoiceElevate
	default:
		return utils.ApprovalChoiceDeny
	}
}

// askForFilesystemSecurityApproval is the matching SelectList-based picker
// for out-of-workspace filesystem accesses.  Item set depends on tier —
// Sensitive paths can't be session-allowlisted, so only "Allow once" / "Deny"
// are offered.
func askForFilesystemSecurityApproval(prompt, path, folder string, tier utils.FilesystemPromptTier) utils.ApprovalChoice {
	return askForFilesystemSecurityApprovalWriter(os.Stdout, prompt, path, folder, tier)
}

func askForFilesystemSecurityApprovalWriter(w io.Writer, prompt, path, folder string, tier utils.FilesystemPromptTier) utils.ApprovalChoice {
	clihooks.SuspendIndicator()
	clihooks.PauseSteer()
	clihooks.SuspendStreaming()
	defer clihooks.ResumeSteer()
	defer clihooks.ResumeStreaming()

	// SP-070-2: Ring bell to alert the user that a blocking approval is needed
	fmt.Fprint(w, "\a")

	writeSecurityHeader(w, prompt, "Path", path)

	var items []SelectItem
	var title string
	switch tier {
	case utils.FilesystemPromptSensitive:
		writeSecurityFootnote(w, "Sensitive location (system dir, or home while CWD is outside home). "+
			"Cannot be session-allowlisted; every access will prompt.")
		items = []SelectItem{
			{Label: "Allow once", Detail: "read/write this path this time only", Value: "approve_once"},
			{Label: "Deny", Detail: "reject and surface a security error", Value: "deny"},
		}
		title = "Sensitive path access — choose an action (Esc denies)"
	default: // FilesystemPromptExternal
		items = []SelectItem{
			{Label: "Allow once", Detail: "read/write this path this time only", Value: "approve_once"},
			{Label: "Deny", Detail: "reject and surface a security error", Value: "deny"},
			{Label: "Allow folder this session", Detail: fmt.Sprintf("auto-approve everything under %s", folder), Value: "allow_folder"},
		}
		title = "External path access — choose an action (Esc denies)"
	}

	sl := NewSelectList(SelectListOptions{
		Title:  title,
		Items:  items,
		Footer: "↑/↓ navigate · Enter confirm · Esc denies",
	})

	value, ok := runApprovalPicker(w, sl)
	if !ok {
		return utils.ApprovalChoiceDeny
	}

	switch value {
	case "approve_once":
		return utils.ApprovalChoiceApproveOnce
	case "deny":
		return utils.ApprovalChoiceDeny
	case "allow_folder":
		return utils.ApprovalChoiceAllowFolderSession
	default:
		return utils.ApprovalChoiceDeny
	}
}

// writeSecurityHeader prints the amber warning glyph + prompt text, then the
// labeled target (Command/Path) on its own indented block.  Keeps the
// visual treatment in one place so the two askFor*SecurityApproval functions
// stay consistent.
func writeSecurityHeader(w io.Writer, prompt, label, target string) {
	useColor := envutil.ResolveColorPreference(true)
	const dim = "\033[2m"
	const reset = "\033[0m"

	fmt.Fprintln(w)
	if useColor {
		fmt.Fprintf(w, "%s%s\n", GlyphWarning.Prefix(), prompt)
		fmt.Fprintf(w, "%s%s:%s\n  %s\n\n", dim, label, reset, target)
	} else {
		fmt.Fprintf(w, "%s%s\n", GlyphWarning.Prefix(), prompt)
		fmt.Fprintf(w, "%s:\n  %s\n\n", label, target)
	}
}

// writeSecurityFootnote renders a single dim-styled line of caveat text
// between the header and the picker.  Falls back to plain text in no-color
// mode.
func writeSecurityFootnote(w io.Writer, note string) {
	useColor := envutil.ResolveColorPreference(true)
	const dim = "\033[2m"
	const reset = "\033[0m"

	if useColor {
		fmt.Fprintf(w, "%s  %s%s\n\n", dim, note, reset)
	} else {
		fmt.Fprintf(w, "  %s\n\n", note)
	}
}

func init() {
	utils.SecurityPromptHook = askForSecurityApproval
	utils.FilesystemSecurityPromptHook = askForFilesystemSecurityApproval
}
