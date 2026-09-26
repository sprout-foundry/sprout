package tools

// ask_user_cli.go — the CLI rendering + interactive input layer for
// ask-user: the TTY / freeform / line readers (stdinIsTTY, readFreeformInput,
// readFreeformInputCtx, readLineCtx), the CLI prompt / framing renderers
// (renderCLIPrompt, renderCLIFraming), the select-list runner
// (runAskUserSelectList), and the CLI option-answer resolution
// (resolveCLIOptionAnswer, matchSingleOption). Split out of ask_user.go.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"

	"golang.org/x/term"
)

// stdinIsTTY reports whether os.Stdin is connected to an interactive
// terminal. Uses the same ioctl-based check (golang.org/x/term.IsTerminal)
// as the security approval prompt and the rest of the agent so the two
// code paths never disagree about whether a TTY is available.
//
// Previously this used os.Stdin.Stat() + os.ModeCharDevice, which can
// diverge from the ioctl result in certain daemon/pipe configurations —
// causing the security dialog to render while ask_user claimed no input
// channel existed.
func stdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// readFreeformInput reads lines from the reader until a blank line is
// encountered (the user pressed Enter on an empty line) or EOF is reached.
// Consecutive newlines within pasted content are preserved — only a blank
// line at the *end* terminates input. The returned string is trimmed of
// trailing whitespace but internal newlines are kept intact.
func readFreeformInput(reader *bufio.Reader) (string, error) {
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				// EOF counts as the terminator — return whatever we have.
				trimmed := strings.TrimRight(line, "\n\r")
				if trimmed != "" {
					lines = append(lines, trimmed)
				}
				return strings.Join(lines, "\n"), nil
			}
			return "", err
		}
		trimmed := strings.TrimRight(line, "\n\r")
		if trimmed == "" && len(lines) > 0 {
			// Blank line signals end of input (but only if we already
			// have content — an immediate blank returns empty string).
			return strings.Join(lines, "\n"), nil
		}
		lines = append(lines, trimmed)
	}
}

// readFreeformInputCtx is the context-aware variant of readFreeformInput.
// It spawns a goroutine to perform the blocking stdin read and selects
// on ctx.Done() so a tool-execution timeout or interrupt cancels the
// read cleanly. The goroutine leaks on ctx cancellation (the blocked
// syscall won't return until the user types something or stdin closes),
// but this is acceptable: the agent process owns stdin and the next
// reader will consume the stray input.
func readFreeformInputCtx(ctx context.Context, reader *bufio.Reader) (string, error) {
	type result struct {
		answer string
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		answer, err := readFreeformInput(reader)
		resultCh <- result{answer, err}
	}()
	select {
	case r := <-resultCh:
		return r.answer, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// readLineCtx reads a single line from reader, cancelling on ctx.Done().
// Same goroutine-leak tradeoff as readFreeformInputCtx.
func readLineCtx(ctx context.Context, reader *bufio.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		resultCh <- result{line, err}
	}()
	select {
	case r := <-resultCh:
		return r.line, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// renderCLIPrompt writes the question and (optionally) the numbered
// option list to w. Kept on a separate function so the tests can call
// it against a buffer.
func renderCLIPrompt(w io.Writer, req AskUserRequest) {
	const bar = "────────────────────────────────────────────────"
	fmt.Fprintln(w)
	fmt.Fprintln(w, bar)
	if h := strings.TrimSpace(req.Header); h != "" {
		fmt.Fprintf(w, "  %s\n", h)
		fmt.Fprintln(w, bar)
	}
	fmt.Fprintf(w, "  %s\n", req.Question)
	if len(req.Options) > 0 {
		fmt.Fprintln(w)
		for i, opt := range req.Options {
			marker := " "
			value := optionValue(opt)
			if req.Default != "" && (req.Default == value || req.Default == opt.Label) {
				marker = "*"
			}
			fmt.Fprintf(w, "  %s %d. %s", marker, i+1, opt.Label)
			if opt.Description != "" {
				fmt.Fprintf(w, "  — %s", opt.Description)
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w)
		if req.MultiSelect {
			fmt.Fprintln(w, "  Enter numbers separated by commas (e.g. 1,3) or labels.")
		} else {
			fmt.Fprintln(w, "  Enter a number, an option label, or your own text.")
		}
	}
	fmt.Fprintln(w, bar)
	if len(req.Options) == 0 {
		if req.Default != "" {
			fmt.Fprintf(w, "> [default: %s] (Enter blank line to submit)\n", req.Default)
		} else {
			fmt.Fprintln(w, "> (Enter blank line to submit)")
		}
	} else if req.Default != "" {
		fmt.Fprintf(w, "> [default: %s] ", req.Default)
	} else {
		fmt.Fprint(w, "> ")
	}
}

// renderCLIFraming writes the question chrome (divider / optional header /
// question / closing divider) to w WITHOUT the option list. The TTY
// single-select path uses this to print the question before launching
// the arrow-key picker, which renders its own options pane.
//
// Kept visually consistent with renderCLIPrompt so users perceive the
// two prompts as the same surface — only the input mechanism differs.
func renderCLIFraming(w io.Writer, req AskUserRequest) {
	const bar = "────────────────────────────────────────────────"
	fmt.Fprintln(w)
	fmt.Fprintln(w, bar)
	if h := strings.TrimSpace(req.Header); h != "" {
		fmt.Fprintf(w, "  %s\n", h)
		fmt.Fprintln(w, bar)
	}
	fmt.Fprintf(w, "  %s\n", req.Question)
	fmt.Fprintln(w, bar)
}

// askUserCustomAnswerSentinel is the SelectItem.Value we attach to the
// "Type your own answer…" item in the single-select picker. When the
// user picks that item, we fall through to the legacy freeform input
// path so the LLM's structured options don't trap the human into a
// canned response. The value is intentionally a magic string (not a
// valid label match) so a real option can never collide with it.
const askUserCustomAnswerSentinel = "__custom__"

// runAskUserSelectList drives the TTY single-select picker. It prints
// the question framing, runs console.SelectList over the options, and
// returns the selected option's Value on confirm. On Esc/Ctrl+C it
// returns a "user cancelled selection" error matching the spirit of
// the legacy prompt (Esc cancels; the default is not auto-applied).
//
// Free-text fallback: the picker gets a trailing "Type your own
// answer…" item. Picking it falls through to the legacy bufio reader
// so the user can still write arbitrary text — the LLM might have
// given options but the human wants to answer differently.
//
// This path is only used when stdin is a TTY and MultiSelect is false;
// callers (AskUser) gate on those conditions before invoking.
func runAskUserSelectList(ctx context.Context, req AskUserRequest) (string, error) {
	items := make([]console.SelectItem, 0, len(req.Options)+1)
	for _, opt := range req.Options {
		items = append(items, console.SelectItem{
			Label:  opt.Label,
			Detail: opt.Description,
			Value:  optionValue(opt),
		})
	}
	// Always offer a free-text escape hatch. The item isn't a real
	// option, so we detect it via the sentinel value below and route
	// the user to the legacy freeform input path.
	items = append(items, console.SelectItem{
		Label:  "Type your own answer…",
		Detail: "write a custom response",
		Value:  askUserCustomAnswerSentinel,
	})

	renderCLIFraming(os.Stdout, req)

	sl := console.NewSelectList(console.SelectListOptions{
		// Title is intentionally empty — renderCLIFraming already shows
		// the question/header above the picker, and SelectList would just
		// duplicate it.
		Title:      "",
		Items:      items,
		Searchable: len(items) > 5,
		PageSize:   10,
	})

	value, ok, err := sl.Run(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		// Esc / Ctrl+C: treat as cancellation. Note this differs from the
		// legacy "Enter on empty → default" behavior — the picker has no
		// notion of "submit blank", so Esc is the only way out without
		// picking. Returning an error surfaces the cancel to the caller
		// (the LLM), which is the right semantic.
		return "", fmt.Errorf("user cancelled selection")
	}

	// Free-text fallback: drop into the legacy reader so the user can
	// write whatever they want. The framing is already on screen; we
	// just append a "> " prompt below the (now-cleared) picker rows.
	if value == askUserCustomAnswerSentinel {
		fmt.Fprint(os.Stdout, "> ")
		reader := bufio.NewReader(os.Stdin)
		answer, err := readFreeformInputCtx(ctx, reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", ErrAskUserNoChannel
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", err
			}
			return "", fmt.Errorf("read user input: %w", err)
		}
		if answer == "" && req.Default != "" {
			return req.Default, nil
		}
		return answer, nil
	}

	return value, nil
}

// resolveCLIOptionAnswer maps the raw user input to an option value (or
// comma-joined values for multi-select). Returns ok=false if the input
// doesn't match any option and there is no sensible freeform fallback.
func resolveCLIOptionAnswer(answer string, req AskUserRequest) (string, bool) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		if req.Default != "" {
			return req.Default, true
		}
		return "", false
	}

	if req.MultiSelect {
		parts := strings.Split(answer, ",")
		var resolved []string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			v, ok := matchSingleOption(part, req.Options)
			if !ok {
				return "", false
			}
			resolved = append(resolved, v)
		}
		if len(resolved) == 0 {
			return "", false
		}
		return strings.Join(resolved, ","), true
	}

	if v, ok := matchSingleOption(answer, req.Options); ok {
		return v, true
	}
	// No option matched — treat as freeform text. The schema permits it.
	return answer, true
}

func matchSingleOption(token string, options []AskUserOption) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	if n, err := strconv.Atoi(token); err == nil {
		if n >= 1 && n <= len(options) {
			return optionValue(options[n-1]), true
		}
		return "", false
	}
	lower := strings.ToLower(token)
	for _, opt := range options {
		if strings.EqualFold(opt.Label, token) || strings.EqualFold(optionValue(opt), token) {
			return optionValue(opt), true
		}
	}
	for _, opt := range options {
		if strings.HasPrefix(strings.ToLower(opt.Label), lower) {
			return optionValue(opt), true
		}
	}
	return "", false
}
