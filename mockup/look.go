package mockup

// Generating the project's look: the step before a mockup of a change to an
// existing product can be drawn in its own colours. It reads the project's
// styling (its CSS, theme files and design tokens), asks the model to record
// the look, and saves it to .sprout/look.json. It runs once per project; a
// draw reads the saved look rather than re-deriving it, so the product's
// colours do not drift between mockups.

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/mockup/kit"
)

//go:embed prompts/mockup-look.md
var lookFile string

// lookCharter is the system prompt for the look step.
func lookCharter() string { return lookFile }

// LookTimeout bounds one look generation.
const (
	// maxLookSources bounds how many styling files are read, and
	// maxLookSourceBytes how much of each: a look is read from a handful of
	// theme files, not from a whole repository.
	maxLookSources     = 24
	maxLookSourceBytes = 24 * 1024
	// maxLookBudget bounds the whole styling corpus handed to the model.
	maxLookBudget = 120 * 1024
)

// lookSourceExtensions are the files a project's look is read from.
var lookSourceExtensions = map[string]bool{
	".css": true, ".scss": true, ".sass": true, ".less": true,
	".json": true, ".js": true, ".ts": true, ".jsx": true, ".tsx": true,
}

// lookSourceNames are file names that carry a project's styling wherever they
// sit (a tailwind config, a theme module, a token file).
var lookSourceNames = map[string]bool{
	"tailwind.config.js": true, "tailwind.config.ts": true, "tailwind.config.cjs": true,
	"theme.js": true, "theme.ts": true, "theme.css": true,
	"globals.css": true, "global.css": true, "variables.css": true, "tokens.css": true,
	"tokens.json": true, "design-tokens.json": true, "theme.json": true,
}

// lookSourceDirs are the directories worth reading styling from, in order.
var lookSourceDirs = []string{"", "src", "app", "styles", "css", "theme", "themes", "design", "public"}

// skippedDirs are never descended into: styling is not in them, and they are
// where a repository is largest. `.sprout` is our own state (the plan, the
// mockup, this look) — never the project's styling.
var skippedDirs = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true, ".next": true,
	"vendor": true, "target": true, ".venv": true, "venv": true, "__pycache__": true,
	"coverage": true, ".cache": true, "out": true, ".sprout": true,
}

// MockupRelPath is the mockup's place in the project.
const MockupRelPath = "design/mockup.html"

// ErrNoStyling is returned by GenerateLook when the project has no styling to
// read: there is no look to record, and the mockup is drawn in a palette of
// the kit's instead.
var ErrNoStyling = errors.New("the project has no styling to read")

// LookResult is a generated look.
type LookResult struct {
	// Look is the recorded look, cleaned.
	Look kit.Look
	// Sources are the files it was read from, project-relative.
	Sources []string
	// Saved is whether it was written to .sprout/look.json.
	Saved bool
}

// GenerateLook reads the project's styling under root, asks the model for the
// look, and saves it. It is the pre-step to drawing a mockup of a change to
// an existing product: a project with no styling yields ErrNoStyling, which
// callers treat as "draw it as a new idea".
func GenerateLook(ctx context.Context, stream StreamText, root string) (LookResult, error) {
	if stream == nil {
		return LookResult{}, errors.New("no model is available to read the project's look")
	}
	sources, corpus := readStyling(root)
	if len(sources) == 0 {
		return LookResult{}, ErrNoStyling
	}
	reply, err := stream(ctx, lookCharter(), lookRequestForSources(corpus), nil)
	if err != nil {
		return LookResult{}, fmt.Errorf("reading the project's look: %w", err)
	}
	look, ok := kit.ParseLook(reply)
	if !ok {
		return LookResult{}, errors.New("the model did not describe a usable look")
	}
	saved, err := kit.SaveLook(root, look)
	if err != nil {
		return LookResult{Look: look, Sources: sources}, fmt.Errorf("saving the project's look: %w", err)
	}
	return LookResult{Look: look, Sources: sources, Saved: saved}, nil
}

// lookRequestForSources is the request for the look step: the styling files,
// each named and quoted.
func lookRequestForSources(corpus string) string {
	return "The project's styling follows, one file at a time.\n\n" + corpus
}

// readStyling collects the project's styling files, project-relative and in a
// stable order, with the corpus the model is shown. It reads at most
// maxLookSources files and maxLookBudget bytes.
func readStyling(root string) ([]string, string) {
	var sources []string
	var b strings.Builder
	budget := maxLookBudget
	seen := map[string]bool{}

	consider := func(path string) {
		if len(sources) >= maxLookSources || budget <= 0 || seen[path] {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			return
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if len(data) > maxLookSourceBytes {
			data = data[:maxLookSourceBytes]
		}
		if len(data) > budget {
			data = data[:budget]
		}
		seen[rel] = true
		sources = append(sources, rel)
		budget -= len(data)
		b.WriteString("### " + rel + "\n")
		b.Write(data)
		if !strings.HasSuffix(string(data), "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Named styling files first: they carry the palette wherever they sit.
	for _, dir := range lookSourceDirs {
		base := root
		if dir != "" {
			base = filepath.Join(root, dir)
		}
		for name := range lookSourceNames {
			consider(filepath.Join(base, name))
		}
	}
	// Then the rest of the styling, by extension, in a stable walk.
	var found []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skippedDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !lookSourceExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		// Our own artifacts are not the project's styling: the mockup we
		// wrote, the look itself.
		switch filepath.ToSlash(path) {
		case filepath.ToSlash(filepath.Join(root, MockupRelPath)),
			filepath.ToSlash(filepath.Join(root, kit.LookRelPath)):
			return nil
		}
		found = append(found, path)
		return nil
	})
	sort.Strings(found)
	for _, path := range found {
		consider(path)
	}
	return sources, b.String()
}
