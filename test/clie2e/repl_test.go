//go:build unix

package clie2e

import (
	"strings"
	"testing"
	"time"
)

const startupTimeout = 30 * time.Second

// waitForPrompt waits until the REPL is idle at its prompt.
func waitForPrompt(s *session) {
	s.t.Helper()
	s.waitFor("the idle prompt", startupTimeout, func(screen string) bool {
		return strings.Contains(screen, "›") || strings.Contains(screen, "> ")
	})
}

// waitForIdleBox waits until the turn-end redraw has settled and the empty
// input box (with its placeholder) is on screen.
func waitForIdleBox(s *session) {
	s.t.Helper()
	s.waitFor("the idle input box", startupTimeout, func(screen string) bool {
		return strings.Contains(screen, "Message sprout")
	})
}

// A first turn: the user's message and the reply land in the transcript, in
// order, once each.
func TestREPL_TurnRendersOnce(t *testing.T) {
	s := start(t, 100, 30, "REPLY-ONE is here")
	waitForPrompt(s)
	s.typeLine("FIRST-QUESTION")
	s.waitForText("REPLY-ONE is here", 20*time.Second)

	screen := s.scr.Text()
	if strings.Count(screen, "REPLY-ONE is here") != 1 {
		t.Fatalf("reply rendered %d times:\n%s", strings.Count(screen, "REPLY-ONE is here"), s.scr.Dump())
	}
	if q, r := strings.Index(screen, "FIRST-QUESTION"), strings.Index(screen, "REPLY-ONE"); q < 0 || q > r {
		t.Fatalf("question must precede the reply:\n%s", s.scr.Dump())
	}
}

// Several turns, enough to scroll: every turn's lines survive in order — the
// scroll region scrolls instead of overwriting the last row.
func TestREPL_ManyTurnsScrollWithoutLosingLines(t *testing.T) {
	replies := make([]string, 6)
	for i := range replies {
		replies[i] = strings.Repeat("filler line\n", 3) + "ANSWER-" + string(rune('A'+i))
	}
	s := start(t, 100, 24, replies...)
	waitForPrompt(s)
	for i := range replies {
		s.typeLine("ASK-" + string(rune('A'+i)))
		s.waitForText("ANSWER-"+string(rune('A'+i)), 20*time.Second)
	}
	screen := s.scr.Text()
	last := strings.Index(screen, "ANSWER-F")
	prev := strings.Index(screen, "ASK-F")
	if prev < 0 || last < prev {
		t.Fatalf("the last turn is not intact and in order:\n%s", s.scr.Dump())
	}
	if !s.scr.CursorInRegion() {
		t.Fatalf("cursor left below the scroll region:\n%s", s.scr.Dump())
	}
}

// SP-171c: resizing the height mid-session keeps the transcript and re-pins
// the footer to the new bottom, shrinking and growing.
//
// Height only: the emulator does not reflow, like xterm. Most terminals
// (iTerm2, Terminal.app, kitty) rewrap rows on a width change, and the
// footer's resize handling assumes they do, so a width change cannot be
// checked against this model.
func TestREPL_ResizeKeepsTranscriptAndFooter(t *testing.T) {
	s := start(t, 100, 30, "BEFORE-RESIZE reply", "AFTER-SHRINK reply", "AFTER-GROW reply")
	waitForPrompt(s)
	s.typeLine("one")
	s.waitForText("BEFORE-RESIZE reply", 20*time.Second)

	for _, step := range []struct {
		rows  int
		reply string
	}{{22, "AFTER-SHRINK reply"}, {34, "AFTER-GROW reply"}} {
		// Resize once the turn-end redraw has settled. A resize landing
		// inside that redraw can strand the old footer rows (SP-171c,
		// open); this test pins the settled case.
		waitForIdleBox(s)
		s.resize(100, step.rows)
		time.Sleep(500 * time.Millisecond)
		s.typeLine("next")
		s.waitForText(step.reply, 20*time.Second)

		screen := s.scr.Text()
		if !strings.Contains(screen, "BEFORE-RESIZE reply") {
			t.Fatalf("resizing to %d rows ate the earlier reply:\n%s", step.rows, s.scr.Dump())
		}
		if strings.Count(screen, step.reply) != 1 {
			t.Fatalf("%q rendered %d times:\n%s", step.reply, strings.Count(screen, step.reply), s.scr.Dump())
		}
		lines := s.scr.Lines()
		if !strings.Contains(strings.Join(lines[len(lines)-4:], "\n"), "─") {
			t.Fatalf("the footer is not pinned to the new bottom at %d rows:\n%s", step.rows, s.scr.Dump())
		}
	}
}

// `!` runs on the terminal: its output reaches the screen and the prompt
// comes back.
func TestREPL_BangCommandRunsAndReturns(t *testing.T) {
	s := start(t, 100, 30)
	waitForPrompt(s)
	s.typeLine("!echo BANG-OUTPUT-$((40+2))")
	s.waitForText("BANG-OUTPUT-42", 15*time.Second)
	waitForPrompt(s)
}

// toolCall is a queued mock reply that calls one tool, in the fenced JSON
// form the agent loop's fallback parser recovers.
func toolCall(prose, name, argsJSON string) string {
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(argsJSON)
	return prose + "\n```json\n{\"tool_calls\":[{\"type\":\"function\",\"function\":{\"name\":\"" + name + "\",\"arguments\":\"" + esc + "\"}}]}\n```"
}

// The original report: after answering a question mid-turn, the output that
// followed was pinned to the bottom and every line overwrote the last. Each
// line after the answer must land on its own row, in order.
func TestREPL_OutputAfterAnsweringAQuestionScrolls(t *testing.T) {
	var after []string
	for i := 1; i <= 8; i++ {
		after = append(after, "AFTER-LINE-"+string(rune('0'+i)))
	}
	s := start(t, 100, 24,
		toolCall("Checking with you.", "ask_user", `{"question":"PICK-A-COLOR?","options":[{"label":"red"},{"label":"blue"}]}`),
		strings.Join(after, "\n"),
	)
	waitForPrompt(s)
	s.typeLine("start")
	s.waitForText("PICK-A-COLOR?", 20*time.Second)
	time.Sleep(700 * time.Millisecond) // past the picker's arm delay
	if _, err := s.ptmx.WriteString("2"); err != nil {
		t.Fatal(err)
	}
	s.waitForText("AFTER-LINE-8", 20*time.Second)
	time.Sleep(300 * time.Millisecond)

	lines := s.scr.Lines()
	row := map[string]int{}
	for i, l := range lines {
		for _, want := range after {
			if strings.Contains(l, want) {
				row[want] = i
			}
		}
	}
	for i, want := range after {
		r, ok := row[want]
		if !ok {
			s.saveRaw()
			t.Fatalf("%s was overwritten or lost:\n%s", want, s.scr.Dump())
		}
		if i > 0 && r <= row[after[i-1]] {
			t.Fatalf("%s is not below %s:\n%s", want, after[i-1], s.scr.Dump())
		}
	}
	if !s.scr.CursorInRegion() {
		t.Fatalf("cursor left below the scroll region:\n%s", s.scr.Dump())
	}
}
