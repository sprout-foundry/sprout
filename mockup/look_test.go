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

// The look pre-step: a project's own colours, type and corners are recorded
// once from the evidence of its styling, and every draw after that is in
// them. A look whose project has since been restyled is recorded again.

const lookReply = `{"colors":{"primary":"#7c3aed","bg":"#0b0d12"},"font_body":"Inter, sans-serif","radius":"6px","dark":true,"notes":"A dark app with a left rail."}`

// dirFiles is a project's files on disk, as the look step reads them.
type dirFiles struct{ root string }

func (d dirFiles) Paths() []string {
	var paths []string
	_ = filepath.WalkDir(d.root, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		if rel, err := filepath.Rel(d.root, p); err == nil {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	return paths
}

func (d dirFiles) Read(p string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(d.root, filepath.FromSlash(p)))
	if err != nil {
		return "", false
	}
	return string(data), true
}

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

func TestLookEvidence_ReadsPropertiesNotWholeFiles(t *testing.T) {
	root := styledProject(t)
	ev := LookEvidence(dirFiles{root})
	if !strings.Contains(ev, "--primary:#7c3aed") {
		t.Fatalf("the custom properties are the evidence: %q", ev)
	}
	if !strings.Contains(ev, "src/globals.css") {
		t.Fatalf("the file is named: %q", ev)
	}
}

func TestLookEvidence_NothingToRead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ev := LookEvidence(dirFiles{root}); ev != "" {
		t.Fatalf("a project with no styling has no evidence: %q", ev)
	}
}

func TestLookEvidence_NamesUILibraries(t *testing.T) {
	root := t.TempDir()
	pkg := `{"dependencies":{"tailwindcss":"^3","@mui/material":"^5"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
	css := ":root{--primary:#fff}\n"
	if err := os.WriteFile(filepath.Join(root, "a.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := LookEvidence(dirFiles{root})
	if !strings.Contains(ev, "tailwindcss") || !strings.Contains(ev, "@mui") {
		t.Fatalf("the UI libraries are named: %q", ev)
	}
}

func TestGenerateLook_SavesAndReadsBack(t *testing.T) {
	root := styledProject(t)
	stream, asked := replies(t, lookReply)
	res, err := GenerateLook(context.Background(), stream, root, dirFiles{root})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Saved || res.Hash == "" {
		t.Fatalf("saved with a hash: %+v", res)
	}
	if !strings.Contains((*asked)[0], "--primary:#7c3aed") {
		t.Fatalf("the model is shown the evidence: %s", (*asked)[0])
	}
	if res.Look.Colors["primary"] != "#7c3aed" || res.Look.Radius != "6px" || !res.Look.Dark {
		t.Fatalf("look: %+v", res.Look)
	}
	saved, err := kit.LoadLook(root)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Look.Colors["primary"] != "#7c3aed" || saved.Look.LookNotes() != "A dark app with a left rail." {
		t.Fatalf("saved: %+v", saved)
	}
}

func TestGenerateLook_NoStyling(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stream, asked := replies(t, lookReply)
	if _, err := GenerateLook(context.Background(), stream, root, dirFiles{root}); !errors.Is(err, ErrNoStyling) {
		t.Fatalf("want ErrNoStyling, got %v", err)
	}
	if len(*asked) != 0 {
		t.Fatal("no model call when there is nothing to read")
	}
}

func TestGenerateLook_UnusableReply(t *testing.T) {
	root := styledProject(t)
	stream, _ := replies(t, "I could not find any colours.")
	if _, err := GenerateLook(context.Background(), stream, root, dirFiles{root}); err == nil {
		t.Fatal("an unusable reply is an error")
	}
}

func TestDraw_RecordsTheLookFirstThenDrawsInIt(t *testing.T) {
	root := styledProject(t)
	product := `<!doctype html><html data-kit="clean" data-palette="project"><head></head><body><section data-screen="board"><h1>Board</h1></section></body></html>`
	stream, asked := replies(t, lookReply, product)
	res, err := Draw(context.Background(), stream, Request{
		Plan: []byte(`{"brief":"board"}`), Root: root, Files: dirFiles{root},
	}, nil)
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
	files := dirFiles{root}
	// Record it once.
	stream, _ := replies(t, lookReply)
	if _, err := GenerateLook(context.Background(), stream, root, files); err != nil {
		t.Fatal(err)
	}
	product := `<!doctype html><html data-kit="clean" data-palette="project"><head></head><body><section data-screen="board"></section></body></html>`
	stream2, asked := replies(t, product)
	if _, err := Draw(context.Background(), stream2, Request{Plan: []byte(`{"brief":"board"}`), Root: root, Files: files}, nil); err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 1 {
		t.Fatalf("a saved look is not re-recorded: %d model calls", len(*asked))
	}
	if !strings.Contains((*asked)[0], "A dark app with a left rail.") {
		t.Fatal("the saved look's notes reach the draw")
	}
}

func TestDraw_RestyledProjectRecordsTheLookAgain(t *testing.T) {
	root := styledProject(t)
	files := dirFiles{root}
	stream, _ := replies(t, lookReply)
	if _, err := GenerateLook(context.Background(), stream, root, files); err != nil {
		t.Fatal(err)
	}
	// The project is restyled: the recorded look no longer matches.
	css := ":root{--primary:#16a34a;--bg:#ffffff}\n"
	if err := os.WriteFile(filepath.Join(root, "src", "globals.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	again := `{"colors":{"primary":"#16a34a","bg":"#ffffff"},"notes":"A light app."}`
	product := `<!doctype html><html data-kit="clean" data-palette="project"><head></head><body><section data-screen="board"></section></body></html>`
	stream2, asked := replies(t, again, product)
	if _, err := Draw(context.Background(), stream2, Request{Plan: []byte(`{"brief":"board"}`), Root: root, Files: files}, nil); err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 2 {
		t.Fatalf("a restyled project is read again: %d model calls", len(*asked))
	}
	if !strings.Contains((*asked)[0], "#16a34a") {
		t.Fatal("the new styling is the evidence")
	}
}

func TestDraw_NoStylingDrawsInTheKitsPalette(t *testing.T) {
	root := t.TempDir()
	stream, asked := replies(t, page)
	if _, err := Draw(context.Background(), stream, Request{
		Plan: []byte(`{"name":"Crumb"}`), Root: root, Files: dirFiles{root},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 1 {
		t.Fatalf("no look step without styling: %d model calls", len(*asked))
	}
}
