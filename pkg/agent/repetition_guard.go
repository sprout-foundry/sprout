// repetition_guard.go — the streaming degenerate-repetition guard.
// A model can occasionally degenerate mid-reply into a repetition loop: the
// same short line ("Let me run." / "Let me do it.") over and over with no
// tool call, until it breaks out on its own. Such a reply is a dead turn:
// nothing is acted on and the user waits out the loop. The guard watches the
// streamed assistant text, recognizes the degenerate run as it forms, and
// signals so the streaming layer can cut the attempt and retry once with a
// nudge.
//
// The component is pure and fully unit-testable: it wraps a sink (the
// client-facing delivery, the same seam the language hold-back uses) and
// exposes Write / Finish / Reset / Looping / Dropped. It reconstructs lines
// from the chunk stream and holds back only the REPEATED occurrences of a
// short line — the first occurrence is legitimate prose and streams through
// per chunk, so live streaming is unchanged for ordinary replies. When a run
// of the same short line reaches the configured threshold the held duplicates
// are discarded (the degenerate text does not reach the client) and Looping()
// turns true; every later chunk is captured but not delivered.
//
// False positives are the hazard, so the guard ignores fenced code blocks
// (repeated lines are normal there), counts only SHORT lines (a repeated long
// paragraph is prose, not degeneration), counts only near-verbatim
// consecutive identical lines (similar-but-different list items do not form
// a run), and treats blank lines as neutral. A progress log whose lines
// differ by a counter or path never accumulates a run.
package agent

import (
	"strings"
)

// RepetitionGuardConfig parameterizes the guard. The zero value is the
// enabled default set; the configured thresholds come from the repetition
// guard section (see pkg/configuration).
type RepetitionGuardConfig struct {
	// Enabled gates the guard. When false the guard is a pure passthrough
	// and never flags.
	Enabled bool
	// MinRepetitions is how many consecutive identical short lines form a
	// degenerate run. A value below 2 is clamped to the default.
	MinRepetitions int
	// MaxLineChars is the longest a line may be to count toward a run.
	// Longer lines are prose, not degenerate tokens, and are skipped
	// (delivered and never counted).
	MaxLineChars int
}

// The default thresholds: a run of six identical short lines is degenerate,
// while four (a plausible list) is not. Short means at most 120 characters —
// past that a repeated line reads as prose, not a stuck token.
const (
	defaultRepetitionMinRepetitions = 6
	defaultRepetitionMaxLineChars   = 120
)

// resolveRepetitionGuardConfig fills a config's zero values with the defaults
// and clamps MinRepetitions to at least 2.
func resolveRepetitionGuardConfig(cfg RepetitionGuardConfig) RepetitionGuardConfig {
	if cfg.MinRepetitions < 2 {
		cfg.MinRepetitions = defaultRepetitionMinRepetitions
	}
	if cfg.MaxLineChars <= 0 {
		cfg.MaxLineChars = defaultRepetitionMaxLineChars
	}
	return cfg
}

// RepetitionGuard observes streamed assistant text and detects a degenerate
// repetition run. It wraps a sink that receives content that is not held back
// as a candidate repeated line.
type RepetitionGuard struct {
	cfg  RepetitionGuardConfig
	sink func(content string)

	// buf is the text seen since the start of the current (unfinished) line,
	// kept only so the line can be reconstructed when the next newline
	// arrives. It is bounded: once the buffered line exceeds MaxLineChars it
	// is known to be too long to withhold, so the guard stops buffering it
	// (longLine) and streams the rest through eagerly.
	buf strings.Builder
	// delivered records how many bytes of buf have already been delivered to
	// the sink (a chunk with no newline streams through eagerly, so its bytes
	// are delivered before the line completes). A line whose prefix was
	// already delivered cannot be withheld and is streamed through.
	delivered int
	// longLine is set once the current line is known to exceed MaxLineChars
	// (too long to be a degenerate token): the guard stops buffering it and
	// streams its remainder through until the newline ends it.
	longLine bool

	// inFence tracks whether the current position is inside a fenced code
	// block, where repeated lines are legitimate.
	inFence bool
	// fenceMarker is the fence marker that opened the current block
	// ("```" or "~~~"), so the matching close is recognized.
	fenceMarker string

	// runLine is the normalized text of the current candidate run's line and
	// runCount how many consecutive occurrences have been seen (including the
	// first, which was already delivered).
	runLine  string
	runCount int
	// pending holds the repeated occurrences of runLine that have not been
	// delivered: they are flushed when the run breaks, discarded when it
	// degenerates.
	pending []string

	// looping is set once a degenerate run is confirmed; the guard then
	// captures but never delivers, and Looping() reports true.
	looping bool
	// dropped accumulates the degenerate text that never reached the sink —
	// the held duplicates at detection plus every later chunk.
	dropped strings.Builder
	// toolCall is set when the response has emitted a tool call; a reply
	// that acts is never a degenerate loop.
	toolCall bool
}

// NewRepetitionGuard constructs a guard that delivers non-degenerate content
// to sink. A disabled config still constructs a usable guard; when disabled
// it is a pure passthrough.
func NewRepetitionGuard(cfg RepetitionGuardConfig, sink func(content string)) *RepetitionGuard {
	return &RepetitionGuard{
		cfg:  resolveRepetitionGuardConfig(cfg),
		sink: sink,
	}
}

// Enabled reports whether the guard is active. A disabled guard never flags
// and delivers every chunk eagerly.
func (g *RepetitionGuard) Enabled() bool { return g.cfg.Enabled }

// Write feeds one chunk of the streamed assistant text. Complete lines are
// evaluated for a degenerate run: the first occurrence of a line streams
// through, repeated occurrences are held back (flushed on a break, discarded
// on degeneration). A disabled guard delivers the chunk eagerly.
func (g *RepetitionGuard) Write(chunk string) {
	if !g.cfg.Enabled {
		g.deliver(chunk)
		return
	}
	if g.looping {
		// The run already degenerated: capture the rest, deliver nothing.
		g.dropped.WriteString(chunk)
		return
	}
	if g.longLine {
		// Inside a line already known to be too long to withhold: deliver it
		// through until the newline ends it, without buffering.
		idx := strings.IndexByte(chunk, '\n')
		if idx < 0 {
			g.deliver(chunk)
			return
		}
		g.deliver(chunk[:idx+1])
		g.longLine = false
		// A long line is neutral: it neither resets nor extends a run, so the
		// remainder of this chunk resumes normal line processing.
		chunk = chunk[idx+1:]
		if chunk == "" {
			return
		}
	}

	g.buf.WriteString(chunk)
	for {
		buffered := g.buf.String()
		idx := strings.IndexByte(buffered, '\n')
		if idx < 0 {
			break
		}
		line := buffered[:idx+1]
		rest := buffered[idx+1:]

		// The bytes of this line that were already delivered (an earlier
		// newline-less chunk) cannot be withheld; only a line whose prefix is
		// still undelivered can be held.
		prefixDelivered := g.delivered

		g.buf.Reset()
		g.buf.WriteString(rest)
		g.delivered = 0

		g.processLine(line, prefixDelivered)
		if g.looping {
			// Anything after the degenerate line is degenerate tail too.
			if g.buf.Len() > 0 {
				g.dropped.WriteString(g.buf.String())
				g.buf.Reset()
				g.delivered = 0
			}
			return
		}
	}

	// No complete line left: stream the trailing partial through per chunk so
	// live streaming is unchanged, and remember how much was delivered so a
	// later completion can tell whether the line is still withholdable.
	if g.buf.Len() > g.cfg.MaxLineChars {
		// The buffered line already exceeds the maximum withholdable length:
		// it cannot be a degenerate token, so deliver its remainder eagerly
		// and stop buffering it until the newline (bounding memory and
		// avoiding an O(n²) copy of an ever-growing buffer).
		g.deliver(g.buf.String()[g.delivered:])
		g.buf.Reset()
		g.delivered = 0
		g.longLine = true
		return
	}
	if g.buf.Len() > g.delivered {
		g.deliver(g.buf.String()[g.delivered:])
		g.delivered = g.buf.Len()
	}
}

// processLine evaluates one complete line (including its trailing newline).
// prefixDelivered is the number of leading bytes of the line already delivered
// to the sink; a positive value means the line was split across chunks and its
// bytes can no longer be withheld.
func (g *RepetitionGuard) processLine(line string, prefixDelivered int) {
	trimmed := strings.TrimSpace(line)

	// Fence tracking first: lines inside a fenced code block are never
	// counted (repeated lines are normal in code and in tool output).
	if marker, ok := fenceMarker(trimmed); ok {
		if !g.inFence {
			g.inFence = true
			g.fenceMarker = marker
		} else if marker == g.fenceMarker {
			g.inFence = false
			g.fenceMarker = ""
		}
		g.flushRun()
		g.deliverLine(line, prefixDelivered)
		return
	}
	if g.inFence {
		g.deliverLine(line, prefixDelivered)
		return
	}

	// Blank lines and long lines are not degenerate tokens: deliver them and
	// leave any in-progress run untouched (a blank line inside a code listing,
	// or padding between repeated short lines, must not by itself reset the
	// run).
	if trimmed == "" || len([]rune(trimmed)) > g.cfg.MaxLineChars {
		g.deliverLine(line, prefixDelivered)
		return
	}

	if trimmed == g.runLine {
		g.runCount++
		if g.runCount >= g.cfg.MinRepetitions {
			// Degenerate: the held duplicates never reached the sink; record
			// them plus this occurrence as the dropped text and stop
			// delivering.
			g.looping = true
			for _, held := range g.pending {
				g.dropped.WriteString(held)
			}
			g.pending = nil
			if prefixDelivered == 0 {
				g.dropped.WriteString(line)
			} else {
				g.deliverLine(line, prefixDelivered)
			}
			return
		}
		if prefixDelivered > 0 {
			// The line was split across chunks and partially delivered: it
			// cannot be withheld, so count the run but stream it through.
			g.deliverLine(line, prefixDelivered)
			return
		}
		// A repeat in progress: hold this occurrence back.
		g.pending = append(g.pending, line)
		return
	}

	// A different short line: flush the previous run (it was legitimate) and
	// start a new one with this line delivered eagerly.
	g.flushRun()
	g.runLine = trimmed
	g.runCount = 1
	g.deliverLine(line, prefixDelivered)
}

// deliverLine delivers a complete line, sending only the part not already
// delivered (a line split across chunks).
func (g *RepetitionGuard) deliverLine(line string, prefixDelivered int) {
	if prefixDelivered >= len(line) {
		return
	}
	g.deliver(line[prefixDelivered:])
}

// flushRun delivers the held duplicates of the current run and clears the run
// state — the run broke, so the repeated lines were legitimate.
func (g *RepetitionGuard) flushRun() {
	for _, line := range g.pending {
		g.deliver(line)
	}
	g.pending = nil
	g.runLine = ""
	g.runCount = 0
}

// Finish flushes the incomplete trailing line and any held (legitimate)
// duplicates, then releases the guard. A finished guard that never degenerated
// has delivered the whole reply; a looping guard delivers nothing further.
func (g *RepetitionGuard) Finish() {
	if !g.cfg.Enabled || g.looping {
		return
	}
	if g.buf.Len() > g.delivered {
		g.deliver(g.buf.String()[g.delivered:])
	}
	g.buf.Reset()
	g.delivered = 0
	g.longLine = false
	g.flushRun()
}

// Reset returns the guard to its initial state for a fresh stream (the
// retry-once attempt), dropping any held, captured, or looping state so the
// discarded attempt never leaks into the next one.
func (g *RepetitionGuard) Reset() {
	g.buf.Reset()
	g.delivered = 0
	g.longLine = false
	g.inFence = false
	g.fenceMarker = ""
	g.runLine = ""
	g.runCount = 0
	g.pending = nil
	g.looping = false
	g.dropped.Reset()
	g.toolCall = false
}

// NoteToolCall records that the response has emitted a tool call: a reply
// that acts is never a degenerate loop, so a noted tool call suppresses
// flagging. The streaming callback cannot see tool-call deltas mid-stream, so
// the provider checks the completed response (see repetitionAttempt): a reply
// that carries tool calls is kept for the turn to act on instead of being cut.
func (g *RepetitionGuard) NoteToolCall() { g.toolCall = true }

// Looping reports whether a degenerate repetition run has been confirmed.
func (g *RepetitionGuard) Looping() bool { return g.looping && !g.toolCall }

// Dropped returns the degenerate text that never reached the sink (empty when
// the guard never flagged).
func (g *RepetitionGuard) Dropped() string { return g.dropped.String() }

// RunLength returns the length of the current consecutive identical-line run.
func (g *RepetitionGuard) RunLength() int { return g.runCount }

// deliver writes content to the sink (a no-op for a nil sink).
func (g *RepetitionGuard) deliver(content string) {
	if g.sink != nil {
		g.sink(content)
	}
}

// fenceMarker reports whether a trimmed line opens or closes a fenced code
// block, and with which marker ("```" or "~~~"). A fence line may carry an
// info string (```go) or trailing text; only the marker's presence matters.
func fenceMarker(trimmed string) (string, bool) {
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, marker) {
			return marker, true
		}
	}
	return "", false
}
