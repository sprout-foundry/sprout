package tools

// ask_user_cli.go — the CLI rendering + interactive input layer for
// ask-user: the TTY / freeform / line readers (stdinIsTTY, readFreeformInput,
// readFreeformInputCtx, readLineCtx), the CLI prompt / framing renderers
// (renderCLIPrompt, renderCLIFraming), the select-list runner
// (runAskUserSelectList), and the CLI option-answer resolution
// (resolveCLIOptionAnswer, matchSingleOption). Split out of ask_user.go.

import (
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

// readAnswer reads the user's typed answer: one Enter submits, a multi-line
// paste arrives whole, and a cancelled read leaves nothing parked on stdin
// to swallow the keystrokes that follow. A variable so tests can drive the
// prompt paths without a terminal.
var readAnswer = console.ReadLineCtx

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
	if req.Default != "" {
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

// ErrAskUserDismissed reports Esc/Ctrl+C on the picker. Worded for the LLM,
// which receives it as the tool result: the user chose not to answer.
var ErrAskUserDismissed = errors.New("the user dismissed the question without answering")

// runAskUserSelectList drives the TTY single-select picker. It prints the
// question framing, runs console.SelectList over the options, and returns
// the selected option's Value on confirm.
//
// Answering in your own words never needs a detour: in a short list (no
// filter) the first nine options carry digit shortcuts and any other key
// leaves the picker for a typed answer that starts with that key; in a long
// list typing filters, and the trailing "Type your own answer…" item does
// the same. Esc reports ErrAskUserDismissed.
//
// This path is only used when stdin is a TTY and MultiSelect is false;
// callers (AskUser) gate on those conditions before invoking.
func runAskUserSelectList(ctx context.Context, req AskUserRequest) (string, error) {
	searchable := len(req.Options)+1 > 5
	items := make([]console.SelectItem, 0, len(req.Options)+1)
	for i, opt := range req.Options {
		item := console.SelectItem{
			Label:  opt.Label,
			Detail: opt.Description,
			Value:  optionValue(opt),
		}
		if !searchable && i < 9 {
			item.Key = rune('1' + i)
		}
		items = append(items, item)
	}
	items = append(items, console.SelectItem{
		Label:  "Type your own answer…",
		Detail: "write a custom response",
		Value:  askUserCustomAnswerSentinel,
	})

	renderCLIFraming(os.Stdout, req)

	footer := "↑↓ move · Enter select · type to filter · Esc dismiss"
	if !searchable {
		footer = fmt.Sprintf("1-%d pick · type to answer in your own words · Esc dismiss", len(req.Options))
	}
	sl := console.NewSelectList(console.SelectListOptions{
		// Title is intentionally empty — renderCLIFraming already shows
		// the question/header above the picker.
		Title:           "",
		Items:           items,
		Searchable:      searchable,
		DismissOnAnyKey: !searchable,
		// An Enter typed into the steer box as the question appears must not
		// pick an option the user never saw.
		ArmDelay: console.ApprovalArmDelay,
		Footer:   footer,
		PageSize: 10,
	})

	value, ok, err := sl.Run(ctx)
	if err != nil {
		// SelectList reports a cancelled context as an error. A deadline is
		// the caller's own bound, so it resolves to the default rather than
		// surfacing as a failure; a genuine interrupt still propagates.
		if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return req.Default, nil
		}
		return "", err
	}
	if !ok {
		if ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return req.Default, nil
		}
		if typed := sl.DismissKey(); typed != "" {
			return readCustomAnswer(ctx, req, typed)
		}
		return "", ErrAskUserDismissed
	}
	if value == askUserCustomAnswerSentinel {
		return readCustomAnswer(ctx, req, "")
	}
	return value, nil
}

// readCustomAnswer reads a free-text answer under the picker. typed is what
// the user had already typed when they left the picker; it is echoed and
// kept as the answer's start. (The line is read in cooked mode, so the
// echoed prefix can't be backspaced over — the cost of not running a second
// line editor for one prompt.)
func readCustomAnswer(ctx context.Context, req AskUserRequest, typed string) (string, error) {
	_, _ = fmt.Fprint(os.Stdout, "> "+typed)
	answer, err := readAnswer(ctx)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", ErrAskUserNoChannel
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return trustDefaultOnDeadline(ctx, req, err)
		}
		return "", fmt.Errorf("read user input: %w", err)
	}
	answer = typed + answer
	if strings.TrimSpace(answer) == "" && req.Default != "" {
		return req.Default, nil
	}
	return answer, nil
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
