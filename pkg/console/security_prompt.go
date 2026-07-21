package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// approvalPicker is the abstraction askForSecurityApproval uses to obtain
// the user's selection. Production wires this to runApprovalPicker (real
// SelectList TTY prompt); tests inject a stub that returns canned values
// without touching stdin. Set via SetApprovalPickerForTest.
var approvalPicker = runApprovalPicker

// SetApprovalPickerForTest swaps the picker used by askForSecurityApproval.
// Returns the previous picker so tests can restore it. Test-only — do not
// call from production code.
func SetApprovalPickerForTest(picker func(w io.Writer, sl *SelectList) (string, bool)) func(w io.Writer, sl *SelectList) (string, bool) {
	prev := approvalPicker
	approvalPicker = picker
	return prev
}

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
// SP-124 Phase 3: when an LLM-derived analysis is supplied (analyzer
// succeeded within its timeout), the summary, modifies description, and
// color-coded recommendation badge render above the picker so the user
// sees the LLM's take before choosing. When the analyzer timed out,
// errored, or wasn't run, the picker renders identically to before.
//
// Returns utils.ApprovalChoiceDeny on Esc / Ctrl-C / non-TTY (safe default).
//
// Registered into utils.SecurityPromptHook via init() below so pkg/utils stays
// leaf-level — no upward import on pkg/console.
func askForSecurityApproval(prompt, command string, analysis *utils.SecurityAnalysisView) utils.ApprovalChoice {
	return askForSecurityApprovalWriter(os.Stdout, prompt, command, analysis)
}

func askForSecurityApprovalWriter(w io.Writer, prompt, command string, analysis *utils.SecurityAnalysisView) utils.ApprovalChoice {
	clihooks.SuspendIndicator()
	clihooks.PauseSteer()
	clihooks.SuspendStreaming()
	defer clihooks.ResumeSteer()
	defer clihooks.ResumeStreaming()

	// SP-070-2: Ring bell to alert the user that a blocking approval is needed
	fmt.Fprint(w, "\a")

	writeSecurityHeader(w, prompt, "Command", command)

	// SP-124 Phase 3: LLM-derived analysis panel. Renders above the picker
	// so the user sees the recommendation *before* choosing an option.
	// Skipped entirely when analysis is nil (timeout, error, or analyzer
	// not run for this command) so legacy callers see no visual change.
	if analysis != nil {
		writeSecurityAnalysisPanel(w, analysis)
	}

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

	value, ok := approvalPicker(w, sl)
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

// writeSecurityAnalysisPanel renders the LLM-derived security analysis
// above the 4-option picker so the user sees the recommendation before
// choosing. Mirrors the WebUI dialog's compact panel (summary, modifies,
// tone-coded recommendation badge) so the CLI experience matches the
// browser tab. Falls back to plain text in no-color mode.
//
// SP-124 Phase 3 + SP-124b Phase 2: when ChainLength > 1, renders a
// per-subcommand chain stepper below the analysis block. The stepper is
// the CLI equivalent of the WebUI dialog's horizontal pills — same data,
// same tone-coded dots, same ordering.
func writeSecurityAnalysisPanel(w io.Writer, a *utils.SecurityAnalysisView) {
	if a == nil {
		return
	}
	useColor := envutil.ResolveColorPreference(true)
	const dim = "\033[2m"
	const bold = "\033[1m"
	const reset = "\033[0m"

	// ANSI color codes for the three recommendation tones. Green/amber/red
	// mirror the WebUI panel — green = approve, amber = review, red = reject.
	const green = "\033[32m"
	const amber = "\033[33m"
	const red = "\033[31m"

	// Choose color by recommendation; anything unrecognised falls back to
	// the amber "review" tone so the user knows the LLM expressed doubt.
	var badgeColor, badgeGlyph, badgeLabel string
	switch a.Recommendation {
	case "approve":
		badgeColor, badgeGlyph, badgeLabel = green, "✓", "Looks safe"
	case "reject":
		badgeColor, badgeGlyph, badgeLabel = red, "✗", "Recommend reject"
	default:
		badgeColor, badgeGlyph, badgeLabel = amber, "⚠", "Review needed"
	}

	if useColor {
		fmt.Fprintf(w, "%s  ┌─ LLM analysis ─────────────────────────────────────────────%s\n", dim, reset)
		fmt.Fprintf(w, "%s  │%s %s%s%s\n", dim, reset, bold, a.Summary, reset)
		if a.Modifies != "" {
			fmt.Fprintf(w, "%s  │%s   Affects: %s\n", dim, reset, a.Modifies)
		}
		fmt.Fprintf(w, "%s  │%s   Risk: %s   %s%s %s%s\n", dim, reset, a.RiskAssessment, badgeColor, badgeGlyph, badgeLabel, reset)
		fmt.Fprintf(w, "%s  └──────────────────────────────────────────────────────────%s\n", dim, reset)
	} else {
		fmt.Fprintf(w, "  ┌─ LLM analysis ─────────────────────────────────────────────\n")
		fmt.Fprintf(w, "  │ %s\n", a.Summary)
		if a.Modifies != "" {
			fmt.Fprintf(w, "  │   Affects: %s\n", a.Modifies)
		}
		fmt.Fprintf(w, "  │   Risk: %s   %s %s\n", a.RiskAssessment, badgeGlyph, badgeLabel)
		fmt.Fprintf(w, "  └──────────────────────────────────────────────────────────\n")
	}

	// SP-124b Phase 2: chain stepper for chained-command analyses. Renders
	// below the existing panel. The stepper is omitted when ChainLength is
	// 1 or 0 so single-command callers see no extra visual noise (regression
	// guard).
	if a.ChainLength > 1 {
		writeSecurityAnalysisChainStepper(w, a)
	}
	fmt.Fprintln(w)
}

// MaxChainStepperVisible is the maximum number of subcommands rendered
// inline in the CLI stepper. Chains longer than this collapse to the
// first MaxChainStepperVisible entries plus a "(+N more)" marker so the
// terminal panel stays scannable. SP-124b Phase 2.
const MaxChainStepperVisible = 3

// MaxChainStepperSubcommandWidth is the per-subcommand truncation width
// in the CLI stepper. Width 80 keeps the panel inside a standard 100-col
// terminal when accounting for the dot prefix and indentation. SP-124b Phase 2.
const MaxChainStepperSubcommandWidth = 80

// writeSecurityAnalysisChainStepper renders the per-subcommand chain
// stepper for CLI users. Each subcommand is on its own line with a
// tone-coded bullet dot prefix; chains longer than MaxChainStepperVisible
// entries collapse to the first three plus a "(+N more)" marker. SP-124b Phase 2.
func writeSecurityAnalysisChainStepper(w io.Writer, a *utils.SecurityAnalysisView) {
	if a == nil || a.ChainLength <= 1 || len(a.ChainSubcommands) == 0 {
		return
	}
	useColor := envutil.ResolveColorPreference(true)
	const dim = "\033[2m"
	const reset = "\033[0m"
	const green = "\033[32m"
	const amber = "\033[33m"
	const red = "\033[31m"

	toneColor := func(tone string) string {
		if !useColor {
			return ""
		}
		switch strings.ToLower(strings.TrimSpace(tone)) {
		case "low":
			return green
		case "high":
			return red
		default:
			return amber
		}
	}
	toneGlyph := func(tone string) string {
		switch strings.ToLower(strings.TrimSpace(tone)) {
		case "low":
			return "●"
		case "high":
			return "●"
		default:
			return "●"
		}
	}

	// Match index lengths defensively — ChainClassifications may be shorter
	// than ChainSubcommands if a caller supplied mismatched slices. Unknown
	// tones default to the amber "moderate" look so the missing entry is
	// visible without claiming "low" safety.
	visible := len(a.ChainSubcommands)
	if visible > MaxChainStepperVisible {
		visible = MaxChainStepperVisible
	}

	if useColor {
		fmt.Fprintf(w, "%s    Chain (%d steps):%s\n", dim, a.ChainLength, reset)
	} else {
		fmt.Fprintf(w, "    Chain (%d steps):\n", a.ChainLength)
	}

	for i := 0; i < visible; i++ {
		sub := truncateForStepper(a.ChainSubcommands[i], MaxChainStepperSubcommandWidth)
		tone := ""
		if i < len(a.ChainClassifications) {
			tone = a.ChainClassifications[i]
		}
		if useColor {
			fmt.Fprintf(w, "%s    [%s●%s] %s\n", dim, toneColor(tone), reset, sub)
		} else {
			fmt.Fprintf(w, "    [%s%s] %s\n", toneGlyph(tone), reset, sub)
		}
	}

	hidden := a.ChainLength - visible
	if hidden > 0 {
		if useColor {
			fmt.Fprintf(w, "%s    (+%d more — see full command above)%s\n", dim, hidden, reset)
		} else {
			fmt.Fprintf(w, "    (+%d more — see full command above)\n", hidden)
		}
	}
}

// truncateForStepper shortens s to maxWidth runes, appending "…" when
// truncation happened. UTF-8 safe (counts runes, not bytes). SP-124b Phase 2.
func truncateForStepper(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	if maxWidth <= 1 {
		return string(runes[:maxWidth])
	}
	return string(runes[:maxWidth-1]) + "…"
}

func init() {
	utils.SecurityPromptHook = askForSecurityApproval
	utils.FilesystemSecurityPromptHook = askForFilesystemSecurityApproval
}
