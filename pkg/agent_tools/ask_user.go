package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"

	"golang.org/x/term"
)

// AskUserOption is a single selectable choice in a structured ask_user
// request. When Value is empty the response carries Label verbatim.
type AskUserOption struct {
	Label       string `json:"label"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
}

// AskUserRequest carries the full prompt payload from the tool layer to
// the CLI / WebUI renderer. Only Question is required.
type AskUserRequest struct {
	Question    string          `json:"question"`
	Header      string          `json:"header,omitempty"`
	Options     []AskUserOption `json:"options,omitempty"`
	MultiSelect bool            `json:"multi_select,omitempty"`
	Default     string          `json:"default,omitempty"`
}

// ErrAskUserNoChannel is returned when no input channel is available
// (no WebUI client, stdin not a TTY / closed). The LLM should treat
// this as a hard signal to make a decision itself rather than retry.
var ErrAskUserNoChannel = errors.New("ask_user: no interactive channel available (no WebUI client connected and stdin is not a TTY)")

// AskUserManager coordinates ask_user requests between the agent
// and the webui. It follows the same pattern as security.ApprovalManager
// but returns string responses instead of bool.
type AskUserManager struct {
	mu      sync.Mutex
	pending map[string]chan string // requestID -> response channel
	timeout time.Duration
}

const DefaultAskUserTimeout = 30 * time.Minute

var (
	globalAskUserManager   *AskUserManager
	globalAskUserManagerMu sync.RWMutex
)

// SetGlobalAskUserManager sets the global singleton (called by webui setup).
//
// Deprecated: use dependency injection via Agent.InjectWebUIManagers instead.
func SetGlobalAskUserManager(mgr *AskUserManager) {
	globalAskUserManagerMu.Lock()
	globalAskUserManager = mgr
	globalAskUserManagerMu.Unlock()
}

// GetGlobalAskUserManager returns the global singleton.
//
// Deprecated: use dependency injection via Agent.InjectWebUIManagers instead.
func GetGlobalAskUserManager() *AskUserManager {
	globalAskUserManagerMu.RLock()
	defer globalAskUserManagerMu.RUnlock()
	return globalAskUserManager
}

// NewAskUserManager creates a new AskUserManager with the default timeout.
func NewAskUserManager() *AskUserManager {
	return &AskUserManager{
		pending: make(map[string]chan string),
		timeout: DefaultAskUserTimeout,
	}
}

var (
	nextAskReqID   int64
	nextAskReqIDMu sync.Mutex
)

func generateAskUserRequestID() string {
	nextAskReqIDMu.Lock()
	defer nextAskReqIDMu.Unlock()
	nextAskReqID++
	return fmt.Sprintf("ask_%d", nextAskReqID)
}

// RequestAskUser publishes an ask_user_request event and blocks until the
// webui responds, a timeout elapses, the context is cancelled, or the event bus is nil.
// Returns the user's text response.
func (m *AskUserManager) RequestAskUser(ctx context.Context, eventBus *events.EventBus, req AskUserRequest, clientID, userID, chatID string) (string, error) {
	if eventBus == nil {
		return "", fmt.Errorf("no event bus available")
	}

	if strings.TrimSpace(req.Question) == "" {
		return "", fmt.Errorf("empty question provided")
	}

	requestID := generateAskUserRequestID()
	responseCh := make(chan string, 1)

	m.mu.Lock()
	m.pending[requestID] = responseCh
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.pending, requestID)
		m.mu.Unlock()
	}()

	payload := events.AskUserRequestEvent(requestID, toEventRequest(req), clientID)
	if trimmed := strings.TrimSpace(userID); trimmed != "" {
		payload["user_id"] = trimmed
	}
	if trimmed := strings.TrimSpace(chatID); trimmed != "" {
		payload["chat_id"] = trimmed
	}
	eventBus.Publish(events.EventTypeAskUserRequest, payload)

	timeout := m.timeout
	if timeout <= 0 {
		timeout = DefaultAskUserTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result, ok := <-responseCh:
		if !ok {
			return "", fmt.Errorf("response channel closed")
		}
		return result, nil
	case <-timer.C:
		log.Printf("Ask user request %s timed out after %v", requestID, timeout)
		return "", fmt.Errorf("user did not respond within %v", timeout)
	case <-ctx.Done():
		log.Printf("Ask user request %s cancelled: %v", requestID, ctx.Err())
		return "", fmt.Errorf("ask_user cancelled: %w", ctx.Err())
	}
}

// RespondToAskUser resolves a pending ask_user request with the user's text response.
// Returns true if the request existed and was responded to, false otherwise.
func (m *AskUserManager) RespondToAskUser(requestID string, response string) bool {
	m.mu.Lock()
	ch, exists := m.pending[requestID]
	m.mu.Unlock()

	if !exists {
		return false
	}

	select {
	case ch <- response:
		return true
	default:
		return false
	}
}

// SetTimeout sets the maximum duration requests will block. A zero or
// negative value resets to the default.
func (m *AskUserManager) SetTimeout(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d <= 0 {
		m.timeout = DefaultAskUserTimeout
	} else {
		m.timeout = d
	}
}

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

// AskUser prompts the user with a question and reads input from stdin.
// Renders options as a numbered list when present and accepts either an
// index, the option label, or the option value as the response.
//
// On a TTY with single-select options (MultiSelect == false), the
// options are rendered as an arrow-key picker (console.SelectList)
// with a trailing "Type your own answer…" item that falls through to
// the legacy freeform input reader. Multi-select prompts and prompts
// on a non-TTY stdin fall back to the numbered list + freeform text
// path so the tool remains scriptable.
//
// Returns ErrAskUserNoChannel if stdin is not a TTY (background daemon,
// closed stdin, piped) so callers can distinguish "no input channel"
// from a transient I/O error.
func AskUser(req AskUserRequest) (string, error) {
	if strings.TrimSpace(req.Question) == "" {
		return "", fmt.Errorf("empty question provided")
	}
	if !stdinIsTTY() {
		return "", ErrAskUserNoChannel
	}
	// SP-048 follow-up: stop any active CLI spinner so it doesn't overwrite
	// the question text on stderr while we render it on stdout.
	clihooks.SuspendIndicator()
	// SP-057 follow-up: pause the SteerInputReader so it releases stdin
	// back to cooked mode. The ask_user tool fires mid-turn, so without
	// this the bufio.Reader below would hit EOF immediately (the steer
	// reader is consuming raw-mode stdin) and the tool would silently
	// return an empty answer.
	clihooks.PauseSteer()
	clihooks.SuspendStreaming()
	defer clihooks.ResumeSteer()
	defer clihooks.ResumeStreaming()

	// TTY single-select path: use the arrow-key picker instead of the
	// numbered list. We only branch here when the input channel is a
	// real TTY AND we're not in multi-select mode (SelectList doesn't
	// support multi-select, and the legacy "1,3" comma-list text input
	// remains the canonical path for that case).
	if len(req.Options) > 0 && !req.MultiSelect {
		return runAskUserSelectList(context.Background(), req)
	}

	renderCLIPrompt(os.Stdout, req)

	reader := bufio.NewReader(os.Stdin)

	if len(req.Options) == 0 {
		// Freeform mode: read until a blank line (double Enter) or EOF.
		// This allows pasting multiline text — a single newline between
		// pasted lines is preserved; the user submits by pressing Enter
		// on an empty line.
		answer, err := readFreeformInput(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", ErrAskUserNoChannel
			}
			return "", fmt.Errorf("read user input: %w", err)
		}
		if answer == "" && req.Default != "" {
			return req.Default, nil
		}
		return answer, nil
	}

	// Option mode: single line is sufficient.
	answer, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", ErrAskUserNoChannel
		}
		return "", fmt.Errorf("read user input: %w", err)
	}
	answer = strings.TrimSpace(answer)

	resolved, ok := resolveCLIOptionAnswer(answer, req)
	if !ok {
		return "", fmt.Errorf("invalid selection %q — expected a number 1-%d, an option label, or one of the option values", answer, len(req.Options))
	}
	return resolved, nil
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
		answer, err := readFreeformInput(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", ErrAskUserNoChannel
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

func toEventRequest(req AskUserRequest) events.AskUserRequest {
	out := events.AskUserRequest{
		Question:    req.Question,
		Header:      req.Header,
		MultiSelect: req.MultiSelect,
		Default:     req.Default,
	}
	if len(req.Options) > 0 {
		out.Options = make([]events.AskUserRequestOption, len(req.Options))
		for i, opt := range req.Options {
			out.Options[i] = events.AskUserRequestOption{
				Label:       opt.Label,
				Value:       opt.Value,
				Description: opt.Description,
			}
		}
	}
	return out
}

func optionValue(opt AskUserOption) string {
	if strings.TrimSpace(opt.Value) != "" {
		return opt.Value
	}
	return opt.Label
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

// AskUserWithEventBus prompts the user with a question using the event bus
// for WebUI mode, falling back to stdin for CLI mode.
func AskUserWithEventBus(ctx context.Context, req AskUserRequest, eventBus *events.EventBus, clientID, userID, chatID string, mgr *AskUserManager) (string, error) {
	if strings.TrimSpace(req.Question) == "" {
		return "", fmt.Errorf("empty question provided")
	}

	// WebUI mode: route through event bus
	if mgr != nil && eventBus != nil {
		log.Printf("[ask_user] Routing through event bus: clientID=%q chatID=%q options=%d", clientID, chatID, len(req.Options))
		return mgr.RequestAskUser(ctx, eventBus, req, clientID, userID, chatID)
	}

	if mgr == nil {
		log.Printf("[ask_user] Global AskUserManager is nil — falling back to stdin (WebUI not initialized?)")
	}
	if eventBus == nil {
		log.Printf("[ask_user] Event bus is nil — falling back to stdin")
	}

	// CLI mode: read from stdin
	return AskUser(req)
}
