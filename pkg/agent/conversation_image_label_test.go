package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestProcessImagesAsMultimodal_MultipleImages_NumberedLabels verifies
// SP-103-B4: when multiple images are pasted, each receives a numbered
// label "[image N of M: …]" so the model can refer back in answers.
// For single-image queries we keep the simpler "[image: …]" form to
// avoid implying there's more.
func TestProcessImagesAsMultimodal_MultipleImages_NumberedLabels(t *testing.T) {
	// Save / restore cwd.
	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origCwd)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	pasteDir := filepath.Join(dir, ".sprout", "pasted-images")
	if err := os.MkdirAll(pasteDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create three distinct valid PNGs.
	for _, name := range []string{"alpha.png", "beta.png", "gamma.png"} {
		p := filepath.Join(pasteDir, name)
		if err := os.WriteFile(p, pngMagic, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	query := "Pasted image saved to disk: ./.sprout/pasted-images/alpha.png\n" +
		"Pasted image saved to disk: ./.sprout/pasted-images/beta.png\n" +
		"Pasted image saved to disk: ./.sprout/pasted-images/gamma.png\n" +
		"Compare these three screenshots."

	a := &Agent{client: &visionSupportingClient{supportsVision: true}}
	images, cleaned, err := a.processImagesAsMultimodal(query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) != 3 {
		t.Fatalf("expected 3 images, got %d", len(images))
	}

	// Each numbered label must be present, in order.
	for i := 1; i <= 3; i++ {
		want := "[image " + intToStrForTest(i) + " of 3"
		if !strings.Contains(cleaned, want) {
			t.Errorf("expected cleaned query to contain %q, got %q", want, cleaned)
		}
	}

	// The original placeholder text should be gone.
	if strings.Contains(cleaned, "Pasted image saved to disk:") {
		t.Errorf("placeholder should be removed, got: %q", cleaned)
	}
}

// TestProcessImagesAsMultimodal_SingleImage_SimpleLabel verifies the
// single-image path keeps the simpler "[image: …]" form (not numbered).
func TestProcessImagesAsMultimodal_SingleImage_SimpleLabel(t *testing.T) {
	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origCwd)

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	pasteDir := filepath.Join(dir, ".sprout", "pasted-images")
	if err := os.MkdirAll(pasteDir, 0o755); err != nil {
		t.Fatal(err)
	}

	p := filepath.Join(pasteDir, "solo.png")
	if err := os.WriteFile(p, pngMagic, 0o644); err != nil {
		t.Fatal(err)
	}

	query := "Pasted image saved to disk: ./.sprout/pasted-images/solo.png — what is this?"
	a := &Agent{client: &visionSupportingClient{supportsVision: true}}
	_, cleaned, err := a.processImagesAsMultimodal(query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(cleaned, "[image: solo.png]") {
		t.Errorf("expected simple label [image: solo.png], got %q", cleaned)
	}
	if strings.Contains(cleaned, " of ") {
		// Should NOT have the multi-image " of N" hint.
		t.Errorf("single-image should not have ' of N' suffix, got %q", cleaned)
	}
}

// intToStrForTest is a minimal int->string without importing strconv.
func intToStrForTest(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// Verify the visionSupportingClient type satisfies api.ClientInterface
// at compile time (no-op, just a shape check).
var _ api.ClientInterface = (*visionSupportingClient)(nil)
