package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

const drawMockupAgentPlan = `{
  "version": 1, "revision": 1,
  "created": "2026-10-03T12:00:00Z", "updated": "2026-10-03T12:00:00Z",
  "goal": "Rosa's Cakes: a site where people order a cake for collection.",
  "scope": [{"id": "order", "title": "Order form"}],
  "steps": [{"scope": "order", "description": "Add the order form"}],
  "acceptance": [{"id": "a1", "scope": "order", "check": "The home page shows the order form", "kind": "page"}],
  "out_of_scope": []
}`

// TestDrawMockup_ThroughTheAgent runs draw_mockup the way the planner does:
// the model call goes through the agent's own client (streamed chunks reach
// the host as mockup_chunk events carrying the agent's metadata), usage is
// booked under the mockup role, and the document lands in design/.
func TestDrawMockup_ThroughTheAgent(t *testing.T) {
	agent, root := newPlannerAgent(t)
	if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sprout", "plan.json"), []byte(drawMockupAgentPlan), 0o644); err != nil {
		t.Fatal(err)
	}

	page := []string{
		"<!doctype html><html><head><style>section{display:none}section:first-of-type{display:block}</style></head><body>",
		"<section data-screen=\"home\"><h1>Rosa's Cakes</h1><button data-go=\"order\">Order a cake</button></section>",
		"<section data-screen=\"order\"><form>…</form></section></body></html>",
	}
	client := NewScriptedClient(&ScriptedResponse{
		Content:      strings.Join(page, ""),
		StreamConfig: &StreamConfig{Chunks: page, ReasoningChunks: []string{"thinking about it"}},
		Usage:        ScriptedTokenUsage{PromptTokens: 900, CompletionTokens: 300, TotalTokens: 1200},
	})
	agent.clientMu.Lock()
	agent.client = client
	agent.clientMu.Unlock()

	bus := events.NewEventBus()
	agent.SetEventBus(bus)
	agent.MergeEventMetadata(map[string]interface{}{"chat_id": "chat-test"})
	received := bus.Subscribe("test")
	t.Cleanup(func() { bus.Unsubscribe("test") })

	if err := agent.checkPersonaConfinement("draw_mockup", map[string]interface{}{}); err != nil {
		t.Fatalf("the planner must be allowed draw_mockup: %v", err)
	}
	content, toolErr := agent.ExecuteToolByName(t.Context(), "draw_mockup", `{}`)
	if toolErr != "" {
		t.Fatalf("draw_mockup failed: %s", toolErr)
	}
	if !strings.Contains(content, "home, order") {
		t.Errorf("result = %q, want the screen ids", content)
	}
	written, err := os.ReadFile(filepath.Join(root, "design", "mockup.html"))
	if err != nil || !strings.HasPrefix(string(written), "<!doctype html>") || strings.Contains(string(written), "thinking") {
		t.Fatalf("written = %q, %v", written, err)
	}

	var types []string
	var chunks strings.Builder
	deadline := time.After(5 * time.Second)
	for done := false; !done; {
		select {
		case ev := <-received:
			if !strings.HasPrefix(ev.Type, "mockup_") {
				continue
			}
			data, _ := ev.Data.(map[string]interface{})
			if data["chat_id"] != "chat-test" {
				t.Errorf("%s does not carry the agent's metadata: %v", ev.Type, data)
			}
			types = append(types, ev.Type)
			if ev.Type == events.EventTypeMockupChunk {
				chunks.WriteString(data["chunk"].(string))
			}
			done = ev.Type == events.EventTypeMockupDone || ev.Type == events.EventTypeMockupError
		case <-deadline:
			t.Fatalf("events so far: %v", types)
		}
	}
	if types[0] != events.EventTypeMockupStart || types[len(types)-1] != events.EventTypeMockupDone {
		t.Errorf("events = %v", types)
	}
	if chunks.String() != strings.Join(page, "") {
		t.Errorf("streamed chunks = %q, want the visible text only", chunks.String())
	}

	var booked bool
	for _, ru := range agent.GetRoleUsage() {
		if ru.Role == "mockup" && ru.PromptTokens == 900 && ru.CompletionTokens == 300 {
			booked = true
		}
	}
	if !booked {
		t.Errorf("the mockup's usage must be booked under the mockup role, got %+v", agent.GetRoleUsage())
	}
}

func TestPlannerPersona_DrawMockupIsTheMockupPath(t *testing.T) {
	agent, _ := newPlannerAgent(t)
	planner := agent.configManager.GetConfig().GetSubagentType(personas.IDPlanner)
	if !containsString(planner.AllowedTools, "draw_mockup") {
		t.Fatal("the planner must hold draw_mockup")
	}
	for _, tool := range []string{"write_file", "edit_file"} {
		if containsString(planner.AllowedTools, tool) {
			t.Errorf("the planner draws through draw_mockup; it must not hold %s", tool)
		}
	}
}
