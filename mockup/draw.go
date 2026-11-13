package mockup

// Drawing a mockup: from the agreed plan, or a change to the current one.
// A change is asked for first as edits to the current document (seconds);
// when the edits do not apply, the document is drawn whole with the change.
// A whole draw streams, so a host shows the first screen within seconds: each
// piece the model writes is passed on (with the kit added once <body> has
// passed), and the finished document is the assembled one, which opens on its
// own.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/mockup/kit"
)

// The events a draw publishes, as a host shows it.
const (
	// EventStart: a draw began ("mode": "edit" or "whole").
	EventStart = "mockup_start"
	// EventChunk: a piece of a whole draw ("seq", "chunk").
	EventChunk = "mockup_chunk"
	// EventDone: the finished document ("mode", "html", "screens").
	EventDone = "mockup_done"
	// EventError: a draw that began could not finish ("message").
	EventError = "mockup_error"
)

// Timeout bounds one draw.
const Timeout = 4 * time.Minute

// StreamText is one model call: the system prompt and the request, with each
// piece of the reply passed to onChunk as it is written (onChunk may be nil).
// It returns the whole reply.
type StreamText func(ctx context.Context, system, user string, onChunk func(string)) (string, error)

// Request is what to draw.
type Request struct {
	// Plan is the agreed plan as JSON; nil when there is none.
	Plan []byte
	// Saved is the current mockup as saved (kit included); empty when there
	// is none yet.
	Saved string
	// Change is the change asked for, in the person's words; Part is the
	// part of a screen it is about, when they pointed at one.
	Change string
	Part   string
	// Look is the product's own look, for a mockup of a change to an
	// existing product: it is drawn in the product's colours, type and
	// corners (kit/look.go). LookNotes say how the product is laid out
	// ("a dark app with a left sidebar and dense tables"). Both are empty
	// for a new idea, which is drawn in a palette of the kit's.
	Look      *kit.Look
	LookNotes string
}

// Result is a finished draw.
type Result struct {
	// HTML is the assembled document, kit included: what is saved and shown.
	HTML string
	// Mode is "edit" when the change was made by edits, else "whole".
	Mode    string
	Screens []string
}

// ErrNothingToDraw is a draw with no plan and no change to a current mockup.
var ErrNothingToDraw = errors.New("there is no plan yet: write the plan first, the mockup is drawn from it")

// Draw draws the mockup. publish (may be nil) receives the draw's events;
// a draw that fails after it began publishes EventError. The error is the
// reason in plain words.
func Draw(ctx context.Context, stream StreamText, req Request, publish func(event string, data map[string]any)) (Result, error) {
	if stream == nil {
		return Result{}, errors.New("no model is available to draw the mockup")
	}
	d := &draw{stream: stream, publish: publish}
	if req.Look != nil {
		if l, ok := req.Look.Clean(); ok {
			d.look, d.lookNotes = &l, strings.TrimSpace(req.LookNotes)
		}
	}
	current := ""
	if req.Saved != "" {
		current = kit.Strip(req.Saved)
	}
	note := ChangeNote(req.Change, req.Part)
	if req.Plan == nil && (current == "" || note == "") {
		return Result{}, ErrNothingToDraw
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	if current != "" && note != "" {
		if source, ok := d.edit(ctx, current, note); ok {
			return d.finish(source, "edit"), nil
		}
		if ctx.Err() != nil {
			return Result{}, d.fail("the change took too long; try again")
		}
	}
	return d.whole(ctx, req.Plan, current, note)
}

type draw struct {
	stream    StreamText
	publish   func(string, map[string]any)
	started   bool
	look      *kit.Look
	lookNotes string
}

func (d *draw) emit(event string, data map[string]any) {
	if d.publish != nil {
		d.publish(event, data)
	}
}

func (d *draw) start(mode string) {
	d.emit(EventStart, map[string]any{"mode": mode})
	d.started = true
}

// edit asks for the change as edits to current and applies them. It reports
// false when the change could not be made that way.
func (d *draw) edit(ctx context.Context, current, note string) (string, bool) {
	d.start("edit")
	reply, err := d.stream(ctx, editCharter(), editRequest(current, note)+lookRequest(d.look, d.lookNotes, true), nil)
	if err != nil {
		return "", false
	}
	edited, err := ApplyEdits(current, reply)
	if err != nil {
		return "", false
	}
	return Clean(edited)
}

// whole streams a whole document: from the plan, or from the current mockup
// with the change.
func (d *draw) whole(ctx context.Context, plan []byte, current, note string) (Result, error) {
	d.start("whole")
	var seq int
	streamer := kit.Streamer{Look: d.look}
	text, err := d.stream(ctx, charter(), wholeRequest(plan, current, note)+lookRequest(d.look, d.lookNotes, false), func(chunk string) {
		seq++
		d.emit(EventChunk, map[string]any{"seq": seq, "chunk": streamer.Feed(chunk)})
	})
	if err != nil && strings.TrimSpace(text) == "" {
		if ctx.Err() != nil {
			return Result{}, d.fail("drawing the mockup took too long; try again")
		}
		return Result{}, d.fail(fmt.Sprintf("the mockup could not be drawn right now (%v); try again in a moment", err))
	}
	source, ok := Clean(text)
	if !ok {
		return Result{}, d.fail("the mockup did not come out as a page; try again")
	}
	return d.finish(source, "whole"), nil
}

// finish adds the kit to the document and publishes it.
func (d *draw) finish(source, mode string) Result {
	if d.look != nil {
		source = kit.WithLook(source, *d.look)
	}
	html := kit.Assemble(source)
	screens := ScreenIDs(html)
	d.emit(EventDone, map[string]any{"mode": mode, "html": html, "screens": screens})
	return Result{HTML: html, Mode: mode, Screens: screens}
}

// fail ends a draw that could not be made, telling the host when it had been
// told the draw began.
func (d *draw) fail(reason string) error {
	if d.started {
		d.emit(EventError, map[string]any{"message": reason})
	}
	return errors.New(reason)
}
