package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// drawMockupHandler implements draw_mockup: it makes the clickable mockup of
// the agreed plan, design/mockup.html, with a model call of its own rather
// than having the agent write the HTML into a tool argument. That way the
// document streams: a host renders each piece as it is written (mockup_*
// events) and shows a first screen within seconds, where a file written
// through write_file appears only once the whole argument is complete.
//
// With no change it draws the document from the current plan
// (.sprout/plan.json). With a change and an existing mockup it asks for
// find/replace edits and applies them (mockup_document.go), falling back to
// drawing the whole document with the change when the edits do not apply.
// The model call goes through the agent's own client (ToolFuncSet
// StreamModelText), so it is routed and metered like the agent's turns.
type drawMockupHandler struct{}

const (
	// mockupRelPath is the mockup's place in the project.
	mockupRelPath = "design/mockup.html"
	// mockupRole books the mockup's model use in the session's cost totals.
	mockupRole = "mockup"
	// mockupTimeout bounds one draw.
	mockupTimeout = 4 * time.Minute
	// maxMockupChange and maxMockupPart bound what a change may say.
	maxMockupChange = 2000
	maxMockupPart   = 500
)

var mockupDrawSeq atomic.Int64

func (h *drawMockupHandler) Name() string { return "draw_mockup" }

func (h *drawMockupHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "draw_mockup",
		Description: "Draw the clickable mockup of the agreed plan as design/mockup.html, shown to the person " +
			"while it is drawn. With no change, it draws the mockup from the current plan (write the plan " +
			"first). With a change, it changes the current mockup: pass the change in their words, and when " +
			"they pointed at one part of a screen, name that part in part. Returns the screen ids when done, " +
			"or why it could not be drawn.",
		Parameters: []ParameterDef{
			{
				Name:        "change",
				Type:        "string",
				Description: "Optional. The change they asked for, as they said it (e.g. \"bigger photos\", \"make the button green\"). Leave out to draw the mockup from the plan.",
			},
			{
				Name:        "part",
				Type:        "string",
				Description: "Optional, with change. The part of a screen the change is about, as they pointed at it (e.g. \"the button “Order a cake” on the home screen\").",
			},
		},
	}
}

func (h *drawMockupHandler) Validate(args map[string]any) error {
	change := extractStringOrEmpty(args, "change")
	part := extractStringOrEmpty(args, "part")
	if len(change) > maxMockupChange {
		return agenterrors.NewValidation(fmt.Sprintf("draw_mockup: change is longer than %d characters", maxMockupChange), nil)
	}
	if len(part) > maxMockupPart {
		return agenterrors.NewValidation(fmt.Sprintf("draw_mockup: part is longer than %d characters", maxMockupPart), nil)
	}
	if strings.TrimSpace(part) != "" && strings.TrimSpace(change) == "" {
		return agenterrors.NewValidation("draw_mockup: part names where a change goes; give the change too", nil)
	}
	return nil
}

func (h *drawMockupHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	if err := h.Validate(args); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	root := env.WorkspaceRoot
	if root == "" {
		return mockupFailure("no project folder is available to draw the mockup in"), nil
	}
	funcs := env.ResolveToolFuncs()
	if funcs == nil || funcs.StreamModelText == nil {
		return mockupFailure("no model is available to draw the mockup"), nil
	}

	planJSON, err := currentPlanJSON(root)
	if err != nil {
		return mockupFailure(err.Error()), nil
	}
	current := ""
	if data, readErr := os.ReadFile(filepath.Join(root, mockupRelPath)); readErr == nil {
		current = string(data)
	}
	note := mockupChangeNote(extractStringOrEmpty(args, "change"), extractStringOrEmpty(args, "part"))
	if planJSON == nil && (current == "" || note == "") {
		return mockupFailure("there is no plan yet: write the plan first, the mockup is drawn from it"), nil
	}

	d := &mockupDraw{
		env:   env,
		funcs: funcs,
		root:  root,
		id:    fmt.Sprintf("mockup-%d-%d", time.Now().UnixNano(), mockupDrawSeq.Add(1)),
	}
	ctx, cancel := context.WithTimeout(ctx, mockupTimeout)
	defer cancel()

	if current != "" && note != "" {
		if html, ok := d.edit(ctx, current, note); ok {
			return d.finish(html, "edit", current), nil
		}
		if ctx.Err() != nil {
			return d.fail("the change took too long; try again"), nil
		}
	}
	return d.drawWhole(ctx, planJSON, current, note), nil
}

// currentPlanJSON returns the stored plan as compact JSON, nil when there is
// none, or an error when the stored plan cannot be read.
func currentPlanJSON(root string) ([]byte, error) {
	plan, err := planstore.New().Load(root)
	if errors.Is(err, planstore.ErrNoPlan) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the plan could not be read (%v); write it again", err)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("the plan could not be read (%v); write it again", err)
	}
	return data, nil
}

// mockupDraw is one draw: its id, and whether a host has been told it began.
type mockupDraw struct {
	env     ToolEnv
	funcs   *ToolFuncSet
	root    string
	id      string
	started bool
}

func (d *mockupDraw) publish(eventType string, data map[string]any) {
	data["mockup_id"] = d.id
	if d.funcs != nil && d.funcs.PublishEvent != nil {
		d.funcs.PublishEvent(eventType, data)
		return
	}
	if d.env.EventBus != nil {
		d.env.EventBus.Publish(eventType, data)
	}
}

func (d *mockupDraw) start(mode string) {
	d.publish(events.EventTypeMockupStart, map[string]any{"mode": mode, "path": mockupRelPath})
	d.started = true
}

// edit asks for the change as edits to current and applies them. It reports
// false when the change could not be made that way.
func (d *mockupDraw) edit(ctx context.Context, current, note string) (string, bool) {
	d.start("edit")
	reply, err := d.funcs.StreamModelText(ctx, mockupRole, mockupEditCharter(), mockupEditRequest(current, note), nil)
	if err != nil {
		return "", false
	}
	edited, err := applyMockupEdits(current, reply)
	if err != nil {
		return "", false
	}
	return cleanMockupHTML(edited)
}

// drawWhole streams a whole document: from the plan, or from the current
// mockup with the change.
func (d *mockupDraw) drawWhole(ctx context.Context, planJSON []byte, current, note string) ToolResult {
	d.start("whole")
	var seq int
	text, err := d.funcs.StreamModelText(ctx, mockupRole, mockupCharter(), mockupWholeRequest(planJSON, current, note), func(chunk string) {
		seq++
		d.publish(events.EventTypeMockupChunk, map[string]any{"seq": seq, "chunk": chunk})
	})
	if err != nil && strings.TrimSpace(text) == "" {
		if ctx.Err() != nil {
			return d.fail("drawing the mockup took too long; try again")
		}
		return d.fail(fmt.Sprintf("the mockup could not be drawn right now (%v); try again in a moment", err))
	}
	html, ok := cleanMockupHTML(text)
	if !ok {
		return d.fail("the mockup did not come out as a page; try again")
	}
	return d.finish(html, "whole", current)
}

// finish writes the document, tells the host, and reports the screens.
func (d *mockupDraw) finish(html, mode, previous string) ToolResult {
	path := filepath.Join(d.root, mockupRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return d.fail(fmt.Sprintf("the mockup could not be saved (%v)", err))
	}
	if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
		return d.fail(fmt.Sprintf("the mockup could not be saved (%v)", err))
	}
	_ = d.funcs.TrackWrite(path, previous, html, previous != "")

	screens := mockupScreenIDs(html)
	d.publish(events.EventTypeMockupDone, map[string]any{
		"mode":    mode,
		"path":    mockupRelPath,
		"html":    html,
		"screens": screens,
	})
	out := "Mockup drawn: " + mockupRelPath + "."
	if len(screens) > 0 {
		out += " Screens: " + strings.Join(screens, ", ") + "."
	}
	if mode == "edit" {
		out = "Mockup changed: " + mockupRelPath + "."
		if len(screens) > 0 {
			out += " Screens: " + strings.Join(screens, ", ") + "."
		}
	}
	return ToolResult{
		Output: out,
		StructuredOut: map[string]any{
			"done":    true,
			"mode":    mode,
			"path":    mockupRelPath,
			"screens": screens,
		},
	}
}

// fail ends a draw that could not be made, telling the host when it had
// been told the draw began.
func (d *mockupDraw) fail(reason string) ToolResult {
	if d.started {
		d.publish(events.EventTypeMockupError, map[string]any{"message": reason})
	}
	return mockupFailure(reason)
}

func mockupFailure(reason string) ToolResult {
	return ToolResult{
		Output:        "draw_mockup: " + reason,
		StructuredOut: map[string]any{"done": false, "reason": reason},
		IsError:       true,
	}
}

func (h *drawMockupHandler) Aliases() []string      { return nil }
func (h *drawMockupHandler) Timeout() time.Duration { return mockupTimeout + time.Minute }
func (h *drawMockupHandler) MaxResultSize() int     { return 0 }
func (h *drawMockupHandler) SafeForParallel() bool  { return false }
func (h *drawMockupHandler) Interactive() bool      { return false }
