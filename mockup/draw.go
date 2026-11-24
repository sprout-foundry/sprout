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
	"math/rand/v2"
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
	// Root is the project's folder and Files its files. When Root is set and
	// Look is nil, the draw reads the project's saved look — generating it
	// first when the project has none, or when its styling has changed since
	// — so a mockup of a change to an existing product is drawn in its own
	// colours. Empty Root draws in a palette of the kit's.
	Root  string
	Files Files
	// Content is the current first look's content file: the fields the
	// person set keep their text through this draw.
	Content *Content
	// Avoid are the choices of the person's recent first looks: the draw
	// picks a different kit, composition and font unless the idea clearly
	// calls for one of them.
	Avoid []kit.Choice
	// Fresh redraws the current mockup in a different look on purpose (a
	// different kit, palette, composition and font), keeping its text,
	// screens and behaviour.
	Fresh bool
	// Keep, when set, is the design a whole redraw of an existing mockup must
	// keep: the draw uses exactly that kit, palette, composition and font
	// instead of choosing, and Avoid is ignored. When nil, a whole redraw
	// with Saved set and Fresh unset keeps Saved's own choice, so an update
	// keeps its design; only Fresh picks anew.
	Keep *kit.Choice
	// Direction is a design direction the draw leans toward when it suits
	// the idea; empty picks one at random, so ideas alike do not all come
	// out alike.
	Direction string
}

// Directions are the design directions a draw leans toward, one at random.
var Directions = []string{
	"bold and graphic: strong colour, big confident type",
	"quiet and elegant: space, fine lines, restraint",
	"warm and handmade: earthy colour, friendly serif type",
	"playful and bright: rounded shapes, cheerful colour",
	"editorial: a magazine feel, large serif headlines",
	"modern and crisp: clean sans type, precise layout",
	"retro: print-poster type, thick outlines",
	"natural and calm: soft organic shapes, gentle colour",
}

// Result is a finished draw.
type Result struct {
	// HTML is the assembled document, kit included: what is saved and shown.
	HTML string
	// Mode is "edit" when the change was made by edits, else "whole".
	Mode    string
	Screens []string
	// Content is the page's text, every piece keyed (content.go).
	Content Content
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
	} else if req.Root != "" {
		// The project's look is a pre-step: read the saved one, and when the
		// project has none yet, record it now so this draw is in the
		// product's own colours. A project with no styling to read has no
		// look, and the mockup is drawn in a palette of the kit's.
		look, notes, err := ensureLook(ctx, stream, req.Root, req.Files)
		if err != nil {
			return Result{}, err
		}
		if look != nil {
			d.look, d.lookNotes = look, notes
		}
	}
	current := ""
	if req.Saved != "" {
		current = StripContent(kit.Strip(req.Saved))
	}
	d.saved = req.Content
	keep := req.Keep
	if keep == nil && req.Saved != "" && !req.Fresh {
		// An update keeps its design: the whole redraw (when the edits do
		// not apply) uses the current mockup's own choice.
		if c := kit.ChoiceOf(req.Saved); c.Uses {
			keep = &c
		}
	}
	if keep != nil {
		// Avoid is ignored: the current look is kept exactly.
		d.steer = keepRequest(*keep)
	} else {
		d.steer = steerRequest(req.Avoid, req.Fresh, current)
		if req.Look == nil && req.Root == "" {
			direction := req.Direction
			if direction == "" {
				direction = Directions[rand.IntN(len(Directions))]
			}
			d.steer += "\n\nFor this one, lean toward a " + direction + " look, where it suits the idea."
			if comps := nudgeCompositions(); len(comps) > 0 {
				d.steer += " Consider a `" + comps[rand.IntN(len(comps))] + "` first screen if the kit you choose offers it."
			}
			d.steer += " Name only a kit, palette, composition and font from the lists.\n"
		}
	}
	note := ChangeNote(req.Change, req.Part)
	if req.Fresh && current != "" && note == "" && keep == nil {
		note = freshNote
	}
	if req.Plan == nil && (current == "" || note == "") {
		return Result{}, ErrNothingToDraw
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	if current != "" && note != "" && !req.Fresh {
		if source, ok := d.edit(ctx, current, note); ok {
			return d.finish(source, "edit"), nil
		}
		if ctx.Err() != nil {
			return Result{}, d.fail("the change took too long; try again")
		}
	}
	return d.whole(ctx, req.Plan, current, note, keep)
}

// ensureLook is the look pre-step: the project's saved look, or one recorded
// now. A project with no styling to read yields no look (not an error): the
// mockup is drawn in a palette of the kit's.
func ensureLook(ctx context.Context, stream StreamText, root string, files Files) (*kit.Look, string, error) {
	saved, err := kit.LoadLook(root)
	if err == nil {
		// A look recorded before the project was restyled is stale: the
		// evidence it was read from no longer matches, so read it again.
		if saved.EvidenceHash == evidenceHash(LookEvidence(files)) {
			look := saved.Look
			return &look, look.LookNotes(), nil
		}
	} else if !errors.Is(err, kit.ErrNoLook) {
		return nil, "", fmt.Errorf("reading the project's look: %w", err)
	}
	res, genErr := GenerateLook(ctx, stream, root, files)
	if genErr != nil {
		if errors.Is(genErr, ErrNoStyling) {
			return nil, "", nil
		}
		return nil, "", genErr
	}
	return &res.Look, res.Look.LookNotes(), nil
}

type draw struct {
	saved     *Content
	stream    StreamText
	publish   func(string, map[string]any)
	started   bool
	look      *kit.Look
	lookNotes string
	// steer is what the draw steers away from (steerRequest).
	steer string
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
func (d *draw) whole(ctx context.Context, plan []byte, current, note string, keep *kit.Choice) (Result, error) {
	d.start("whole")
	var seq int
	streamer := kit.Streamer{Look: d.look}
	text, err := d.stream(ctx, charter(), wholeRequest(plan, current, note)+lookRequest(d.look, d.lookNotes, false)+d.steer, func(chunk string) {
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
	if keep != nil {
		// A kept design must be kept exactly, whatever the model chose.
		source = kit.WithChoice(source, *keep)
		if d.look == nil && keep.Look != nil {
			source = kit.WithLook(source, *keep.Look)
		}
	}
	return d.finish(source, "whole"), nil
}

// finish adds the kit to the document and publishes it.
func (d *draw) finish(source, mode string) Result {
	if d.look != nil {
		source = kit.WithLook(source, *d.look)
	}
	source, content := Keyed(source, d.saved)
	html := WithContent(kit.Assemble(source), content)
	screens := ScreenIDs(html)
	d.emit(EventDone, map[string]any{"mode": mode, "html": html, "screens": screens, "content": content})
	return Result{HTML: html, Mode: mode, Screens: screens, Content: content}
}

// fail ends a draw that could not be made, telling the host when it had been
// told the draw began.
func (d *draw) fail(reason string) error {
	if d.started {
		d.emit(EventError, map[string]any{"message": reason})
	}
	return errors.New(reason)
}

// freshNote is the change a different look on purpose asks for.
const freshNote = "Draw this in a different look: another kit, palette, first-screen composition and heading font than it has now. Keep every text and its data-c attribute, the same screens and the same behaviour."

// steerRequest tells the draw what to steer away from: the person's recent
// looks, and, for a different look on purpose, the current one. It names the
// kits, palettes and compositions to leave out this time.
func steerRequest(avoid []kit.Choice, fresh bool, current string) string {
	if fresh && current != "" {
		avoid = append([]kit.Choice{kit.ChoiceOf(current)}, avoid...)
	}
	if len(avoid) > 4 {
		avoid = avoid[:4]
	}
	var kits, palettes, comps []string
	add := func(list *[]string, v string) {
		if v == "" {
			return
		}
		for _, x := range *list {
			if x == v {
				return
			}
		}
		*list = append(*list, v)
	}
	for _, c := range avoid {
		add(&kits, c.Kit)
		add(&palettes, strings.TrimSuffix(strings.TrimSuffix(c.Palette, "-soft"), "-dark"))
		add(&comps, c.Composition)
	}
	if len(kits) == 0 {
		return ""
	}
	out := "\n\nThis person's recent first looks used these, so this one must look different: use a kit other than " + strings.Join(kits, ", ")
	if len(palettes) > 0 {
		out += "; a palette other than " + strings.Join(palettes, ", ")
	}
	if len(comps) > 0 {
		out += "; a first-screen composition other than " + strings.Join(comps, ", ")
	}
	if fresh {
		return out + ".\n"
	}
	return out + " — unless the idea plainly needs one of them.\n"
}

// keepRequest tells the model the design it must keep: the exact kit,
// palette, composition and font, so a whole redraw does not re-choose.
func keepRequest(c kit.Choice) string {
	var parts []string
	if c.Kit != "" {
		parts = append(parts, `data-kit="`+c.Kit+`"`)
	}
	if c.Palette != "" {
		parts = append(parts, `data-palette="`+c.Palette+`"`)
	}
	if c.Composition != "" {
		parts = append(parts, `data-composition="`+c.Composition+`"`)
	}
	if c.Font != "" {
		parts = append(parts, `data-font="`+c.Font+`"`)
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\nKeep the current design exactly: put " + strings.Join(parts, ", ") + " on <html>. Do not choose a different kit, palette, composition or font.\n"
}

// nudgeCompositions are the compositions a draw may be nudged toward: all but
// the art panel, which only ever comes from the drawing choosing it, so a
// nudge never pulls a first screen away from the business's own content.
func nudgeCompositions() []string {
	var out []string
	for _, id := range kit.CompositionIDs() {
		if id != "art" {
			out = append(out, id)
		}
	}
	return out
}
