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

	"github.com/sprout-foundry/sprout/mockup"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
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
// find/replace edits, falling back to drawing the whole document with the
// change when the edits do not apply. The drawing itself is the mockup
// module's (mockup.Draw), which the platform draws with too; this tool reads
// the plan and the saved mockup and writes the result. The model call goes
// through the agent's own client (ToolFuncSet StreamModelText), so it is
// routed and metered like the agent's turns.
type drawMockupHandler struct{}

const (
	// mockupRelPath is the mockup's place in the project.
	mockupRelPath = "design/mockup.html"
	// mockupRole books the mockup's model use in the session's cost totals.
	mockupRole = "mockup"
	// mockupTimeout bounds one draw.
	mockupTimeout = mockup.Timeout
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
	saved := ""
	if data, readErr := os.ReadFile(filepath.Join(root, mockupRelPath)); readErr == nil {
		saved = string(data)
	}

	d := &mockupDraw{
		env:   env,
		funcs: funcs,
		id:    fmt.Sprintf("mockup-%d-%d", time.Now().UnixNano(), mockupDrawSeq.Add(1)),
	}
	stream := func(ctx context.Context, system, user string, onChunk func(string)) (string, error) {
		return funcs.StreamModelText(ctx, mockupRole, system, user, onChunk)
	}
	res, err := mockup.Draw(ctx, stream, mockup.Request{
		Plan:   planJSON,
		Saved:  saved,
		Change: extractStringOrEmpty(args, "change"),
		Part:   extractStringOrEmpty(args, "part"),
	}, d.publish)
	if err != nil {
		return mockupFailure(err.Error()), nil
	}
	return d.save(root, res, saved), nil
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

// mockupDraw is one draw, as the host sees it: its events carry its id and
// the mockup's place in the project.
type mockupDraw struct {
	env   ToolEnv
	funcs *ToolFuncSet
	id    string
}

func (d *mockupDraw) publish(eventType string, data map[string]any) {
	data["mockup_id"] = d.id
	if eventType == mockup.EventStart || eventType == mockup.EventDone {
		data["path"] = mockupRelPath
	}
	if d.funcs != nil && d.funcs.PublishEvent != nil {
		d.funcs.PublishEvent(eventType, data)
		return
	}
	if d.env.EventBus != nil {
		d.env.EventBus.Publish(eventType, data)
	}
}

// save writes the drawn document into the project and reports its screens.
// previous is the file as it was, for the change record.
func (d *mockupDraw) save(root string, res mockup.Result, previous string) ToolResult {
	path := filepath.Join(root, mockupRelPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return mockupFailure(fmt.Sprintf("the mockup could not be saved (%v)", err))
	}
	if err := os.WriteFile(path, []byte(res.HTML), 0o644); err != nil {
		return mockupFailure(fmt.Sprintf("the mockup could not be saved (%v)", err))
	}
	_ = d.funcs.TrackWrite(path, previous, res.HTML, previous != "")

	out := "Mockup drawn: " + mockupRelPath + "."
	if res.Mode == "edit" {
		out = "Mockup changed: " + mockupRelPath + "."
	}
	if len(res.Screens) > 0 {
		out += " Screens: " + strings.Join(res.Screens, ", ") + "."
	}
	return ToolResult{
		Output: out,
		StructuredOut: map[string]any{
			"done":    true,
			"mode":    res.Mode,
			"path":    mockupRelPath,
			"screens": res.Screens,
		},
	}
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
