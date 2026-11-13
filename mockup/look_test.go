package mockup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/mockup/kit"
)

// The look step: a project's own colours, type and corners are recorded once
// (from its styling) and every draw after that is in them. These tests pin the
// pre-step behaviour: the saved look is reused, a missing one is generated and
// saved, and a project with no styling is drawn in the kit's palette.

const lookReply = `{"colors":{"primary":"#7c3aed","bg":"#0b0d12"},"font_body":"Inter, sans-serif","radius":"6px","dark":true,"notes":"A dark app with a left rail."}`

func styledProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	css := ":root{--primary:#7c3aed;--bg:#0b0d12;--radius:6px;font-family:Inter, sans-serif}\n"
	if err := os.WriteFile(filepath.Join(root, "src", "globals.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestGenerateLook_ReadsStylingAndSaves(t *testing.T) {
	root := styledProject(t)
	stream, asked := replies(t, lookReply)
	res, err := GenerateLook(context.Background(), stream, root)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Saved {
		t.Fatal("the look is saved")
	}
	if !strings.Contains((*asked)[0], "--primary:#7c3aed") {
		t.Fatalf("the model is shown the project's styling: %s", (*asked)[0])
	}
	if res.Look.Colors["primary"] != "#7c3aed" || res.Look.Radius != "6px" || !res.Look.Dark {
		t.Fatalf("look: %+v", res.Look)
	}
	// It is on disk, and reads back the same.
	saved, err := kit.LoadLook(root)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Colors["primary"] != "#7c3aed" || saved.LookNotes() != "A dark app with a left rail." {
		t.Fatalf("saved look: %+v", saved)
	}
}

func TestGenerateLook_ProjectWithoutStyling(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stream, asked := replies(t, lookReply)
	if _, err := GenerateLook(context.Background(), stream, root); !errors.Is(err, ErrNoStyling) {
		t.Fatalf("want ErrNoStyling, got %v", err)
	}
	if len(*asked) != 0 {
		t.Fatal("no model call when there is nothing to read")
	}
}

func TestGenerateLook_UnusableReplyIsAnError(t *testing.T) {
	root := styledProject(t)
	stream, _ := replies(t, "I could not find any colours.")
	if _, err := GenerateLook(context.Background(), stream, root); err == nil {
		t.Fatal("an unusable reply is an error")
	}
}

func TestDraw_RecordsTheLookFirstThenDrawsInIt(t *testing.T) {
	root := styledProject(t)
	product := `<!doctype html><html data-kit="clean" data-palette="project"><head></head><body><section data-screen="board"><h1>Board</h1></section></body></html>`
	// First reply is the look, second is the document.
	stream, asked := replies(t, lookReply, product)
	res, err := Draw(context.Background(), stream, Request{Plan: []byte(`{"brief":"board"}`), Root: root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 2 {
		t.Fatalf("the look is a pre-step: %d model calls", len(*asked))
	}
	if !strings.Contains((*asked)[1], "A dark app with a left rail.") {
		t.Fatalf("the draw is told the product's layout: %s", (*asked)[1])
	}
	if !strings.Contains(res.HTML, "--primary:#7c3aed;") || !strings.Contains(res.HTML, `name="mockupkit-look"`) {
		t.Fatal("the page is drawn in the product's look")
	}
}

func TestDraw_ReusesTheSavedLook(t *testing.T) {
	root := styledProject(t)
	if _, err := kit.SaveLook(root, kit.Look{Colors: map[string]string{"primary": "#111111"}, Notes: "A light app."}); err != nil {
		t.Fatal(err)
	}
	product := `<!doctype html><html data-kit="clean" data-palette="project"><head></head><body><section data-screen="board"></section></body></html>`
	stream, asked := replies(t, product)
	if _, err := Draw(context.Background(), stream, Request{Plan: []byte(`{"brief":"board"}`), Root: root}, nil); err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 1 {
		t.Fatalf("a saved look is not regenerated: %d model calls", len(*asked))
	}
	if !strings.Contains((*asked)[0], "A light app.") {
		t.Fatal("the saved look's notes reach the draw")
	}
}

func TestDraw_NoStylingDrawsInTheKitsPalette(t *testing.T) {
	root := t.TempDir() // no styling, no look
	stream, asked := replies(t, page)
	if _, err := Draw(context.Background(), stream, Request{Plan: []byte(`{"name":"Crumb"}`), Root: root}, nil); err != nil {
		t.Fatal(err)
	}
	// One call: the draw itself. No look step, because there is nothing to read.
	if len(*asked) != 1 {
		t.Fatalf("no look step without styling: %d model calls", len(*asked))
	}
	if strings.Contains((*asked)[0], "existing product") {
		t.Fatal("no product look for a project without styling")
	}
}

func TestReadStyling_IgnoresOurOwnState(t *testing.T) {
	// A project with only .sprout/ state (a plan, a mockup) has no styling:
	// reading our own files as the project's look would describe the wrong
	// thing entirely.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sprout", "plan.json"), []byte(`{"goal":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "design"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "design", "mockup.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	sources, _ := readStyling(root)
	if len(sources) != 0 {
		t.Fatalf("our own state is not styling: %v", sources)
	}
}

func TestReadStyling_SkipsHeavyDirectories(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"node_modules/pkg", "dist"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "x.css"), []byte(":root{--a:#fff}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "app.css"), []byte(":root{--b:#000}"), 0o644); err != nil {
		t.Fatal(err)
	}
	sources, _ := readStyling(root)
	if len(sources) != 1 || sources[0] != "app.css" {
		t.Fatalf("only the project's own styling: %v", sources)
	}
}
