package mockup

// Generating the project's look: the step before a mockup of a change to an
// existing product can be drawn in its own colours. It reads the evidence of
// the project's styling (its CSS, theme files, tokens and UI libraries), asks
// the model to record the look, and saves it to .sprout/look.json. It runs
// once per project, and again only when that styling changes; a draw reads
// the saved look rather than re-deriving it, so the product's colours do not
// drift between mockups.

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/sprout-foundry/sprout/mockup/kit"
)

//go:embed prompts/mockup-look.md
var lookFile string

// lookCharter is the system prompt for the look step.
func lookCharter() string { return lookFile }

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
	// Saved is whether it was written to .sprout/look.json.
	Saved bool
	// Hash identifies the evidence it was read from, so a look whose project
	// has since been restyled is recognised as stale.
	Hash string
}

// GenerateLook reads the project's look from the files under root, asks the
// model for it, and saves it. It is the pre-step to drawing a mockup of a
// change to an existing product: a project with no styling yields
// ErrNoStyling, which callers treat as "draw it as a new idea".
func GenerateLook(ctx context.Context, stream StreamText, root string, files Files) (LookResult, error) {
	if stream == nil {
		return LookResult{}, errors.New("no model is available to read the project's look")
	}
	evidence := LookEvidence(files)
	if evidence == "" {
		return LookResult{}, ErrNoStyling
	}
	hash := evidenceHash(evidence)
	reply, err := stream(ctx, lookCharter(), LookPrompt(evidence), nil)
	if err != nil {
		return LookResult{}, fmt.Errorf("reading the project's look: %w", err)
	}
	look, ok := kit.ParseLook(reply)
	if !ok {
		return LookResult{}, errors.New("the model did not describe a usable look")
	}
	saved, err := kit.SaveLook(root, look, hash)
	if err != nil {
		return LookResult{Look: look, Hash: hash}, fmt.Errorf("saving the project's look: %w", err)
	}
	return LookResult{Look: look, Saved: saved, Hash: hash}, nil
}

// evidenceHash identifies the styling a look was read from.
func evidenceHash(evidence string) string {
	sum := sha256.Sum256([]byte(evidence))
	return hex.EncodeToString(sum[:8])
}
