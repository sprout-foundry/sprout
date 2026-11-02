package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

const mockupTestPlan = `{
  "version": 1, "revision": 1,
  "created": "2026-10-03T12:00:00Z", "updated": "2026-10-03T12:00:00Z",
  "goal": "Muddy Paws: a site where people book a dog wash.",
  "scope": [{"id": "book", "title": "Booking form"}],
  "steps": [{"scope": "book", "description": "Add the booking form"}],
  "acceptance": [{"id": "a1", "scope": "book", "check": "The home page shows the booking form", "kind": "page"}],
  "out_of_scope": []
}`

const sampleMockupPage = "<!doctype html><html><body><section data-screen=\"home\"><h1>Muddy Paws</h1></section><section data-screen=\"book\">Book</section></body></html>"

type mockupEvent struct {
	Type string
	Data map[string]any
}

// stubMockupModel answers an edit request with editReply and any other
// request with wholeReply streamed in wholeChunks pieces; it records what it
// was asked and every event the tool published.
type stubMockupModel struct {
	mu          sync.Mutex
	editReply   string
	wholeReply  string
	wholeErr    error
	wholeChunks int
	edits       int
	wholes      int
	roles       []string
	lastUser    string
	events      []mockupEvent
}

func (m *stubMockupModel) funcs() *ToolFuncSet {
	return &ToolFuncSet{
		StreamModelText: func(ctx context.Context, role, system, user string, onChunk func(string)) (string, error) {
			m.mu.Lock()
			m.roles = append(m.roles, role)
			m.lastUser = user
			isEdit := strings.Contains(system, "You change a clickable mockup")
			if isEdit {
				m.edits++
			} else {
				m.wholes++
			}
			m.mu.Unlock()
			if isEdit {
				if onChunk != nil {
					onChunk(m.editReply)
				}
				return m.editReply, nil
			}
			n := max(1, m.wholeChunks)
			size := (len(m.wholeReply) + n - 1) / n
			for i := 0; i < len(m.wholeReply); i += size {
				if onChunk != nil {
					onChunk(m.wholeReply[i:min(len(m.wholeReply), i+size)])
				}
			}
			return m.wholeReply, m.wholeErr
		},
		PublishEvent: func(eventType string, data map[string]any) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.events = append(m.events, mockupEvent{eventType, data})
		},
	}
}

func (m *stubMockupModel) eventTypes() []string {
	out := []string{}
	for _, e := range m.events {
		out = append(out, e.Type)
	}
	return out
}

func mockupProject(t *testing.T, plan bool, current string) string {
	t.Helper()
	root := t.TempDir()
	if plan {
		if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".sprout", "plan.json"), []byte(mockupTestPlan), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if current != "" {
		if err := os.MkdirAll(filepath.Join(root, "design"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, mockupRelPath), []byte(current), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runDrawMockup(t *testing.T, root string, model *stubMockupModel, args map[string]any) ToolResult {
	t.Helper()
	res, err := (&drawMockupHandler{}).Execute(context.Background(), ToolEnv{WorkspaceRoot: root, ToolFuncs: model.funcs()}, args)
	if err != nil {
		t.Fatalf("Execute returned an error (failures must be tool results): %v", err)
	}
	return res
}

func readMockup(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, mockupRelPath))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDrawMockup_FromPlanStreamsAndWrites(t *testing.T) {
	root := mockupProject(t, true, "")
	model := &stubMockupModel{wholeReply: "Here it is:\n```html\n" + sampleMockupPage + "\n```", wholeChunks: 4}

	res := runDrawMockup(t, root, model, map[string]any{})
	if res.IsError {
		t.Fatalf("draw failed: %s", res.Output)
	}
	if got := readMockup(t, root); got != sampleMockupPage {
		t.Fatalf("written = %q, want the cleaned document", got)
	}
	if !strings.Contains(res.Output, "home, book") {
		t.Errorf("result = %q, want the screen ids", res.Output)
	}
	if !strings.Contains(model.lastUser, "Muddy Paws") || !strings.Contains(model.lastUser, "Write the mockup.") {
		t.Errorf("the request must carry the plan, got %.120q", model.lastUser)
	}
	if model.roles[0] != mockupRole {
		t.Errorf("usage role = %q, want %q", model.roles[0], mockupRole)
	}

	types := model.eventTypes()
	if types[0] != events.EventTypeMockupStart || types[len(types)-1] != events.EventTypeMockupDone {
		t.Fatalf("events = %v, want start … done", types)
	}
	var chunks strings.Builder
	id := model.events[0].Data["mockup_id"]
	for i, e := range model.events {
		if e.Data["mockup_id"] != id || id == "" {
			t.Errorf("event %d mockup_id = %v, want %v", i, e.Data["mockup_id"], id)
		}
		if e.Type == events.EventTypeMockupChunk {
			if e.Data["seq"] != countChunks(model.events[:i+1]) {
				t.Errorf("chunk seq = %v", e.Data["seq"])
			}
			chunks.WriteString(e.Data["chunk"].(string))
		}
	}
	if countChunks(model.events) != 4 || !strings.Contains(chunks.String(), sampleMockupPage) {
		t.Errorf("chunks = %d %q, want the streamed reply", countChunks(model.events), chunks.String())
	}
	if model.events[0].Data["mode"] != "whole" {
		t.Errorf("start mode = %v", model.events[0].Data["mode"])
	}
	done := model.events[len(model.events)-1].Data
	if done["html"] != sampleMockupPage || strings.Join(done["screens"].([]string), ",") != "home,book" || done["path"] != mockupRelPath {
		t.Errorf("done = %v", done)
	}
}

func countChunks(evs []mockupEvent) int {
	n := 0
	for _, e := range evs {
		if e.Type == events.EventTypeMockupChunk {
			n++
		}
	}
	return n
}

func TestDrawMockup_ChangeByEdits(t *testing.T) {
	current := "<!doctype html><html><body>\n  <section data-screen=\"home\">\n    <h1>Muddy Paws</h1>\n  </section>\n</body></html>"
	root := mockupProject(t, true, current)
	model := &stubMockupModel{editReply: "<<<FIND\n<h1>Muddy Paws</h1>\n===\n    <h1>Muddy Paws Grooming</h1>\n>>>"}

	res := runDrawMockup(t, root, model, map[string]any{"change": "call it Muddy Paws Grooming", "part": "the heading on the home screen"})
	if res.IsError {
		t.Fatalf("change failed: %s", res.Output)
	}
	if got := readMockup(t, root); !strings.Contains(got, "<h1>Muddy Paws Grooming</h1>") || !strings.Contains(got, "data-screen=\"home\"") {
		t.Fatalf("changed = %q", got)
	}
	if model.edits != 1 || model.wholes != 0 {
		t.Fatalf("edits=%d wholes=%d, want one edit and nothing drawn whole", model.edits, model.wholes)
	}
	if !strings.Contains(model.lastUser, "On the heading on the home screen: call it Muddy Paws Grooming") {
		t.Errorf("edit request must pin the change to its part, got %.200q", model.lastUser)
	}
	if got := strings.Join(model.eventTypes(), ","); got != "mockup_start,mockup_done" {
		t.Errorf("edit-mode events = %s, want start and done only", got)
	}
	if model.events[0].Data["mode"] != "edit" || model.events[1].Data["mode"] != "edit" {
		t.Errorf("edit-mode events must say mode edit: %v", model.events)
	}
}

func TestDrawMockup_EditsThatDoNotApplyFallBackToWhole(t *testing.T) {
	current := "<!doctype html><html><body><section data-screen=\"home\"><p>x</p><p>x</p></section></body></html>"
	for name, reply := range map[string]string{
		"not there": "<<<FIND\n<h2>not there</h2>\n===\n<h2>x</h2>\n>>>",
		"ambiguous": "<<<FIND\n<p>x</p>\n===\n<p>y</p>\n>>>",
		"no edits":  "I changed it for you.",
	} {
		t.Run(name, func(t *testing.T) {
			root := mockupProject(t, true, current)
			model := &stubMockupModel{editReply: reply, wholeReply: sampleMockupPage, wholeChunks: 2}
			res := runDrawMockup(t, root, model, map[string]any{"change": "something else"})
			if res.IsError {
				t.Fatalf("fallback failed: %s", res.Output)
			}
			if readMockup(t, root) != sampleMockupPage {
				t.Fatal("the fallback must write the whole new document")
			}
			if model.edits != 1 || model.wholes != 1 {
				t.Fatalf("edits=%d wholes=%d, want the edit tried, then the whole document", model.edits, model.wholes)
			}
			if !strings.Contains(model.lastUser, "The current mockup:") || !strings.Contains(model.lastUser, "Write the whole updated document.") {
				t.Errorf("the whole request must carry the current mockup and the change, got %.200q", model.lastUser)
			}
			types := model.eventTypes()
			if types[0] != "mockup_start" || model.events[0].Data["mode"] != "edit" {
				t.Errorf("first event = %v", model.events[0])
			}
			if types[1] != "mockup_start" || model.events[1].Data["mode"] != "whole" {
				t.Errorf("the fallback must restart the draw with mode whole, got %v", model.events[1])
			}
			if countChunks(model.events) != 2 || types[len(types)-1] != "mockup_done" {
				t.Errorf("events = %v", types)
			}
		})
	}
}

func TestDrawMockup_Failures(t *testing.T) {
	t.Run("no plan", func(t *testing.T) {
		model := &stubMockupModel{wholeReply: sampleMockupPage}
		res := runDrawMockup(t, mockupProject(t, false, ""), model, map[string]any{})
		if !res.IsError || !strings.Contains(res.Output, "write the plan first") || model.wholes != 0 || len(model.events) != 0 {
			t.Fatalf("res=%q wholes=%d events=%v", res.Output, model.wholes, model.eventTypes())
		}
	})
	t.Run("not a page", func(t *testing.T) {
		root := mockupProject(t, true, "")
		model := &stubMockupModel{wholeReply: "Sorry, I cannot draw that."}
		res := runDrawMockup(t, root, model, map[string]any{})
		if !res.IsError || !strings.Contains(res.Output, "did not come out as a page") {
			t.Fatalf("res = %q", res.Output)
		}
		if types := model.eventTypes(); types[len(types)-1] != events.EventTypeMockupError {
			t.Errorf("events = %v, want an error last", types)
		}
		if _, err := os.Stat(filepath.Join(root, mockupRelPath)); !os.IsNotExist(err) {
			t.Error("nothing must be written when the draw fails")
		}
	})
	t.Run("model error", func(t *testing.T) {
		model := &stubMockupModel{wholeErr: errors.New("upstream unavailable")}
		res := runDrawMockup(t, mockupProject(t, true, ""), model, map[string]any{})
		if !res.IsError || !strings.Contains(res.Output, "could not be drawn right now") {
			t.Fatalf("res = %q", res.Output)
		}
	})
	t.Run("cut short keeps what was written", func(t *testing.T) {
		root := mockupProject(t, true, "")
		model := &stubMockupModel{wholeReply: sampleMockupPage, wholeErr: errors.New("stream ended")}
		if res := runDrawMockup(t, root, model, map[string]any{}); res.IsError {
			t.Fatalf("a document that arrived whole must be kept: %s", res.Output)
		}
	})
	t.Run("no model", func(t *testing.T) {
		res, _ := (&drawMockupHandler{}).Execute(context.Background(), ToolEnv{WorkspaceRoot: mockupProject(t, true, ""), ToolFuncs: &ToolFuncSet{}}, map[string]any{})
		if !res.IsError || !strings.Contains(res.Output, "no model") {
			t.Fatalf("res = %q", res.Output)
		}
	})
	t.Run("part without change", func(t *testing.T) {
		if err := (&drawMockupHandler{}).Validate(map[string]any{"part": "the button"}); err == nil {
			t.Fatal("a part without a change must be refused")
		}
	})
}

func TestDrawMockup_ChangeWithoutPlanUsesCurrentMockup(t *testing.T) {
	// An existing project may have a mockup and no plan: a change still works.
	current := "<!doctype html><html><body><section data-screen=\"home\"><h1>Shop</h1></section></body></html>"
	root := mockupProject(t, false, current)
	model := &stubMockupModel{editReply: "<<<FIND\n<h1>Shop</h1>\n===\n<h1>Corner Shop</h1>\n>>>"}
	if res := runDrawMockup(t, root, model, map[string]any{"change": "call it Corner Shop"}); res.IsError {
		t.Fatalf("change failed: %s", res.Output)
	}
	if !strings.Contains(readMockup(t, root), "Corner Shop") {
		t.Fatal("the change must be applied")
	}
}

func TestDrawMockup_Registered(t *testing.T) {
	if _, ok := GetNewToolRegistry().Lookup("draw_mockup"); !ok {
		t.Fatal("draw_mockup must be in the shared tool registry")
	}
}
