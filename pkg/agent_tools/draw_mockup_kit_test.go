package tools

import (
	"context"
	"strings"
	"testing"

	mockupkit "github.com/sprout-foundry/sprout/mockup/kit"
	"github.com/sprout-foundry/sprout/pkg/events"
)

const kitMockupPage = `<!doctype html><html lang="en" data-kit="studio" data-palette="deep-sea-dark"><head><title>Muddy Paws</title></head>` +
	`<body><section data-screen="home"><h1 class="display">Muddy Paws</h1><svg class="i"><use href="#i-calendar"/></svg></section>` +
	`<section data-screen="book">Book</section></body></html>`

const kitStart = "<!--mockupkit:start-->"

func TestDrawMockup_KitIsStreamedAndSaved(t *testing.T) {
	root := mockupProject(t, true, "")
	model := &stubMockupModel{wholeReply: kitMockupPage, wholeChunks: 5}
	var system string
	funcs := model.funcs()
	inner := funcs.StreamModelText
	funcs.StreamModelText = func(ctx context.Context, role, sys, user string, onChunk func(string)) (string, error) {
		system = sys
		return inner(ctx, role, sys, user, onChunk)
	}
	res, err := (&drawMockupHandler{}).Execute(context.Background(), ToolEnv{WorkspaceRoot: root, ToolFuncs: funcs}, map[string]any{})
	if err != nil || res.IsError {
		t.Fatalf("draw failed: %v %s", err, res.Output)
	}

	if !strings.Contains(system, "## The kit's catalogue") || !strings.Contains(system, "- studio:") || !strings.Contains(system, "- craft:") {
		t.Errorf("the charter must carry the kit's catalogue with every kit's brief")
	}
	saved := readMockup(t, root)
	if saved != mockupkit.Assemble(kitMockupPage) || strings.Count(saved, kitStart) != 1 {
		t.Fatalf("saved document must be the assembled one, got %.200q", saved)
	}
	if !strings.Contains(saved, `<symbol id="i-calendar"`) {
		t.Errorf("saved document must carry the icons it uses")
	}

	var streamed strings.Builder
	for _, e := range model.events {
		if e.Type == events.EventTypeMockupChunk {
			streamed.WriteString(e.Data["chunk"].(string))
		}
	}
	got := streamed.String()
	if strings.Count(got, kitStart) != 1 || strings.Index(got, kitStart) < strings.Index(got, "<body>") {
		t.Errorf("the stream must carry the kit once, just after <body>")
	}
	done := model.events[len(model.events)-1].Data
	if done["html"] != saved {
		t.Errorf("done html must be the saved document")
	}
}

func TestDrawMockup_ChangeIsMadeToTheSource(t *testing.T) {
	root := mockupProject(t, true, mockupkit.Assemble(kitMockupPage))
	model := &stubMockupModel{editReply: "<<<FIND\n<h1 class=\"display\">Muddy Paws</h1>\n===\n<h1 class=\"display\">Muddy Paws Grooming</h1>\n>>>"}

	res := runDrawMockup(t, root, model, map[string]any{"change": "call it Muddy Paws Grooming"})
	if res.IsError {
		t.Fatalf("change failed: %s", res.Output)
	}
	if strings.Contains(model.lastUser, kitStart) {
		t.Errorf("the model must be given the document without the kit")
	}
	saved := readMockup(t, root)
	if strings.Count(saved, kitStart) != 1 || !strings.Contains(saved, "Muddy Paws Grooming") {
		t.Fatalf("changed document must be assembled once with the change, got %.200q", saved)
	}
}
