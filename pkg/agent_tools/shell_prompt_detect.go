//go:build !js

package tools

import (
	"regexp"
	"strings"
)

// Interactive prompts come in two shapes, and they need different handling:
//
//   - Secret prompts (password, passphrase) — the answer must not be echoed
//     and must be redacted from captured output. passwordPromptRe handles
//     these; the WebUI renders a masked dialog.
//
//   - Non-secret prompts — a trailing "[y/N]", "Continue?", "Enter a value:",
//     "Press any key". The answer is ordinary text that belongs in the
//     transcript. These were previously undetected, so a command that asked
//     one blocked forever: no surface ever saw the prompt, and stdin had no
//     writer.
//
// Detection is deliberately conservative. A false positive interrupts a
// command that was going to finish on its own; a false negative is the hang
// we are fixing. The patterns therefore require the prompt to look like a
// question addressed to the terminal at the very end of the pending buffer,
// and genericPromptRe excludes lines that are clearly program output (a
// sentence ending in a colon after a long body of text, a URL, a path).

// genericPromptRe matches a trailing interactive prompt that is not a secret.
//
// The pattern is anchored to the end of the string it is given. Both callers
// matter: the settle timer tests the un-terminated tail (a prompt with no
// trailing newline, like sudo's), and flushCompleteLines tests each completed
// line (a prompt the child wrote with a newline, like `echo "Continue? [y/N]"`).
// The keyword list below is narrow for that reason — a completed line is
// ordinary output most of the time, so only imperatives addressed to the user
// qualify.
var genericPromptRe = regexp.MustCompile(`(?i)(?:` +
	// yes/no confirmations: [y/N], (yes/no), y/n, [Y/n]
	`\[[yYnN]/?[yYnN]?\]|\([yYnN][eE][sS]/[nN][oO]\)|[yYnN]/[yYnN]` +
	// explicit questions
	`|(?:continue|proceed|overwrite|replace|delete|confirm|are you sure|` +
	`do you want to|would you like to|press (?:any key|enter))[^\n]{0,40}\?` +
	`|press (?:any key|enter) to continue` +
	// value prompts: an imperative label ending in a colon. The keyword list is
	// deliberately narrow — "enter", "input", "provide", "type", "choose",
	// "select" are addressed to the user, whereas nouns like "file:", "user:",
	// "type error:" are ordinary output and must not open a dialog.
	`|(?:^|\n)\s*(?:enter|input|provide|type in|choose|select|please enter|` +
	`please choose|please select)[^\n:]{0,60}:\s*$` +
	`)\s*$`)

// genericPromptMaxLabelLen bounds how much trailing text genericPromptRe will
// consider. A colon-terminated line longer than this is treated as output (a
// log line, a sentence) rather than a prompt, which keeps a stray colon in
// prose from triggering a dialog.
const genericPromptMaxLabelLen = 120

// isSecretPrompt reports whether s ends with a password/passphrase prompt.
func isSecretPrompt(s string) bool {
	return passwordPromptRe.MatchString(s)
}

// isGenericPrompt reports whether s ends with a non-secret interactive
// prompt. Secret prompts are matched by isSecretPrompt instead, because the
// answer needs masking; a secret prompt also matches some generic patterns
// ("Password:"), so callers must test secret first.
func isGenericPrompt(s string) bool {
	if s == "" {
		return false
	}
	// Only inspect the trailing line: the prompt is what the child is waiting
	// on, and earlier lines are already-flushed output.
	if idx := strings.LastIndexByte(s, '\n'); idx >= 0 {
		s = s[idx+1:]
	}
	s = strings.TrimRight(s, " \t\r")
	if s == "" {
		return false
	}
	if len(s) > genericPromptMaxLabelLen {
		return false
	}
	// A URL or a filesystem path ending in ":" is output, not a question.
	if strings.Contains(s, "://") {
		return false
	}
	return genericPromptRe.MatchString(s)
}

// isInteractivePrompt reports whether s ends with any prompt the user must
// answer, and whether that answer is secret.
func isInteractivePrompt(s string) (prompt bool, secret bool) {
	if isSecretPrompt(s) {
		return true, true
	}
	if isGenericPrompt(s) {
		return true, false
	}
	return false, false
}
