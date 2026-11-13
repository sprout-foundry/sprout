package mockup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/mockup/kit"
)

const page = `<!doctype html><html data-kit="soft" data-palette="sage"><body><section data-screen="home"><h1>Crumb</h1></section><section data-screen="menu"></section></body></html>`

// replies answers each model call in turn, streaming whole replies in pieces.
func replies(t *testing.T, answers ...string) (StreamText, *[]string) {
	t.Helper()
	var asked []string
	return func(_ context.Context, system, user string, onChunk func(string)) (string, error) {
		asked = append(asked, user)
		if len(asked) > len(answers) {
			return "", errors.New("no answer")
		}
		a := answers[len(asked)-1]
		if onChunk != nil {
			for i := 0; i < len(a); i += 40 {
				end := min(i+40, len(a))
				onChunk(a[i:end])
			}
		}
		return a, nil
	}, &asked
}

type events []string

func (e *events) publish(event string, _ map[string]any) { *e = append(*e, event) }

func TestDraw_WholeFromThePlanStreams(t *testing.T) {
	stream, _ := replies(t, "Here it is:\n"+page)
	var got events
	res, err := Draw(context.Background(), stream, Request{Plan: []byte(`{"name":"Crumb"}`)}, got.publish)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "whole" || strings.Join(res.Screens, ",") != "home,menu" {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(res.HTML, "<h1>Crumb</h1>") || res.HTML == page {
		t.Fatal("the result is the assembled document, kit included")
	}
	if got[0] != EventStart || got[len(got)-1] != EventDone || !strings.Contains(strings.Join(got, ","), EventChunk) {
		t.Fatalf("events = %v", got)
	}
}

func TestDraw_InTheProductsOwnLook(t *testing.T) {
	product := `<!doctype html><html data-kit="clean" data-palette="project"><head></head><body><section data-screen="board"><h1>Board</h1></section></body></html>`
	stream, asked := replies(t, product)
	look := &kit.Look{Colors: map[string]string{"primary": "#7c3aed", "bg": "#0b0d12"}, FontBody: "Inter, sans-serif", Dark: true}
	res, err := Draw(context.Background(), stream, Request{Plan: []byte(`{"brief":"archive cards"}`), Look: look, LookNotes: "a dark app with a left sidebar"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains((*asked)[0], `data-palette="project"`) || !strings.Contains((*asked)[0], "a dark app with a left sidebar") {
		t.Fatalf("the model is told about the product: %s", (*asked)[0])
	}
	if !strings.Contains(res.HTML, "--primary:#7c3aed;") || !strings.Contains(res.HTML, `name="mockupkit-look"`) {
		t.Fatal("the finished page carries the product's look")
	}
	// A new idea is drawn as before: nothing about a product.
	stream, asked = replies(t, page)
	if _, err := Draw(context.Background(), stream, Request{Plan: []byte(`{"name":"Crumb"}`)}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains((*asked)[0], "existing product") {
		t.Fatal("no product look for a new idea")
	}
}

func TestDraw_AChangeIsMadeByEdits(t *testing.T) {
	stream, asked := replies(t, "<<<FIND\n<h1>Crumb</h1>\n===\n<h1>Fresh Crumb</h1>\n>>>")
	res, err := Draw(context.Background(), stream, Request{Saved: page, Change: "rename it"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "edit" || !strings.Contains(res.HTML, "Fresh Crumb") || len(*asked) != 1 {
		t.Fatalf("result = %s, asked %d", res.Mode, len(*asked))
	}
}

func TestDraw_EditsThatDoNotApplyDrawItWhole(t *testing.T) {
	changed := strings.Replace(page, "Crumb", "Fresh Crumb", 1)
	stream, asked := replies(t, "<<<FIND\n<h2>Nope</h2>\n===\nx\n>>>", changed)
	res, err := Draw(context.Background(), stream, Request{Saved: page, Change: "rename it", Part: "the title"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != "whole" || !strings.Contains(res.HTML, "Fresh Crumb") {
		t.Fatalf("result = %+v", res.Mode)
	}
	if !strings.Contains((*asked)[1], "On the title: rename it") {
		t.Fatalf("the whole draw is asked for the change: %q", (*asked)[1])
	}
}

func TestDraw_NeedsAPlanOrAMockupToChange(t *testing.T) {
	stream, _ := replies(t)
	if _, err := Draw(context.Background(), stream, Request{Change: "bigger"}, nil); !errors.Is(err, ErrNothingToDraw) {
		t.Fatalf("err = %v", err)
	}
}

func TestDraw_AFailureAfterItBeganSaysSo(t *testing.T) {
	stream, _ := replies(t, "I could not draw that.")
	var got events
	_, err := Draw(context.Background(), stream, Request{Plan: []byte(`{}`)}, got.publish)
	if err == nil || !strings.Contains(err.Error(), "did not come out as a page") {
		t.Fatalf("err = %v", err)
	}
	if got[len(got)-1] != EventError {
		t.Fatalf("events = %v", got)
	}
}
