//go:build !js

package tools

import (
	"strings"
	"sync"
)

// promptGate decides which stream may ask the user for a prompt.
//
// A command that prints its prompt on both stdout and stderr ("Password:" on
// each) surfaces the same question twice. The answer belongs to the question,
// not to the stream that reported it, so the second report must not ask again.
//
// The rule is deliberately timing-free: the same prompt text reported by both
// streams is ONE question. Whichever stream reports it first asks; the other
// stream's report — whether it arrives before the answer (the same question in
// flight) or after it (a delayed copy, possibly following other output) — is
// refused. The same stream asking again is a new question and is allowed:
// commands prompt their tty, which is one stream, so genuine repeats arrive on
// the stream that asked before.
//
// There is no requirement that both streams report: a command that only ever
// prompts on one stream is never held up waiting for the other.
//
// The tradeoff: a command that asks the identical prompt text on opposite
// streams as separate questions drops the second one. That shape is
// contrived — a re-prompt normally reaches the same tty — while a prompt
// echoed to both streams is common, and any output-based distinction between
// "delayed copy" and "genuine re-ask" reopens the race this gate exists to
// close (measured: the delayed copy regularly lands after the answer's echo).
type promptGate struct {
	mu sync.Mutex
	// lastReported is the prompt text each stream most recently reported.
	lastReported map[int]string
}

func newPromptGate() *promptGate {
	return &promptGate{lastReported: make(map[int]string)}
}

// pipeOther returns the stream that is not pipe.
func pipeOther(pipe int) int {
	if pipe == pipeStdout {
		return pipeStderr
	}
	return pipeStdout
}

// claim reports whether this stream may ask for promptText now.
//
// It refuses when the other stream has reported the same prompt text — that is
// the same question, surfaced twice — and records this stream's report either
// way, so a later report from the other stream is refused in turn.
func (g *promptGate) claim(pipe int, promptText string) bool {
	text := normalizePromptText(promptText)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lastReported[pipeOther(pipe)] == text {
		return false
	}
	g.lastReported[pipe] = text
	return true
}

// normalizePromptText reduces a detected prompt to a comparable form, so the
// same question reported on stdout and stderr (which may differ in trailing
// whitespace or line ending) compares equal.
func normalizePromptText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}
