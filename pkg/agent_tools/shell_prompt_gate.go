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
// A report is a duplicate when the other stream reported the same prompt and
// that report has not been answered. Output in between is the signal the
// command moved on, so the same text afterwards is a new question (sudo's
// "Sorry, try again." then a fresh "Password:").
//
// There is no requirement that both streams report: a command that only
// prompts on one stream must never be held up waiting for the other.
//
// The tradeoff: a command that asks the same question twice back-to-back, on
// opposite streams, with no output in between, is indistinguishable from one
// question reported twice — and is treated as the duplicate. That shape is
// rare (a re-prompt normally prints something, as sudo does) and the
// alternative is asking twice for every prompt a command echoes to both
// streams, which is the common case.
type promptGate struct {
	mu sync.Mutex
	// lastReported is the prompt text each stream most recently reported.
	lastReported map[int]string
	// answered is the prompt text most recently answered.
	answered string
	// movedOn is set when a complete non-prompt line is flushed after an
	// answer, meaning the command moved past the question.
	movedOn bool
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
// It refuses the case the gate exists for: the other stream reported this same
// prompt and it has not been answered yet — the same question surfaced on both
// streams. Once the question is answered, the same text from the other stream
// is a new question only if the command moved on; otherwise it is the other
// stream's copy of the question just answered.
func (g *promptGate) claim(pipe int, promptText string) bool {
	text := normalizePromptText(promptText)
	g.mu.Lock()
	defer g.mu.Unlock()
	if other := g.lastReported[pipeOther(pipe)]; other == text {
		// The other stream reported this text. If it is unanswered, this is
		// the same question in flight. If it is answered, it is a duplicate
		// unless the command has since moved on.
		if g.answered != text || !g.movedOn {
			return false
		}
	}
	g.lastReported[pipe] = text
	return true
}

// release records that pipe's report of promptText was answered. The answer
// closes the question for both streams, so a duplicate report of the same text
// cannot ask again until the command moves on.
func (g *promptGate) release(pipe int, promptText string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.answered = normalizePromptText(promptText)
	g.lastReported[pipe] = g.answered
	g.movedOn = false
}

// observe records that the command produced a complete non-prompt line, so it
// moved past the question and a later report of the same text is a new one.
//
// It is called per complete line rather than per read chunk: a chunk can split
// a prompt across reads, and a fragment is not evidence the command moved on.
func (g *promptGate) observe() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.movedOn = true
}

// normalizePromptText reduces a detected prompt to a comparable form, so the
// same question reported on stdout and stderr (which may differ in trailing
// whitespace or line ending) compares equal.
func normalizePromptText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}
