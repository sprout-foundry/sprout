package agent

import (
	"testing"

	core "github.com/sprout-foundry/seed/core"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// visionMockClient wraps MockClient to report vision support, which is
// required for attachPastedImages to attach images.
type visionMockClient struct {
	MockClient
}

func (v *visionMockClient) SupportsVision() bool { return true }

// TestAttachPastedImages_AttachesToCurrentTurnUserMessage is the core
// regression test for the webui image-paste bug. The sproutProvider must
// attach images registered via RegisterPastedImages to the CURRENT turn's
// user message (the last user message in the request) — attaching them to an
// old turn disconnects the pixels from the question and flips the provider
// prompt-cache prefix. Before the fix, RegisterPastedImages was never called
// by seed_query.go, so pasted images silently never reached the model.
func TestAttachPastedImages_AttachesToCurrentTurnUserMessage(t *testing.T) {
	provider, err := NewSproutProvider(nil, &visionMockClient{
		MockClient{model: "vision-model"},
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	sp := provider.(*sproutProvider)

	// No images registered — messages should pass through unchanged.
	plain := []core.Message{
		{Role: "user", Content: "hello"},
	}
	out := sp.attachPastedImages(plain)
	if len(out) != 1 || len(out[0].Images) != 0 {
		t.Fatalf("expected no images when none registered, got %d messages, first has %d images", len(out), len(out[0].Images))
	}

	// Register an image and verify it is attached.
	testImage := api.ImageData{Base64: "dGVzdA==", Type: "image/png"}
	sp.RegisterPastedImages(map[string][]api.ImageData{
		"_current": {testImage},
	})

	messages := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "describe [image: foo.png]"},
		{Role: "assistant", Content: "ok"},
	}

	out = sp.attachPastedImages(messages)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}

	// Images must be attached to the user message (index 1), not the system
	// message or assistant message.
	if len(out[0].Images) != 0 {
		t.Errorf("system message should have no images, got %d", len(out[0].Images))
	}
	if len(out[1].Images) != 1 {
		t.Fatalf("expected the user message to have 1 image, got %d", len(out[1].Images))
	}
	if out[1].Images[0].Base64 != testImage.Base64 {
		t.Errorf("expected image base64 %q, got %q", testImage.Base64, out[1].Images[0].Base64)
	}
	if len(out[2].Images) != 0 {
		t.Errorf("assistant message should have no images, got %d", len(out[2].Images))
	}
}

// TestAttachPastedImages_MultiTurn_AttachesToLatestUserMessage pins that a
// paste in a continuing conversation lands on the turn being answered, not on
// the historical first user message — and not on a steer message seed may
// append at a later loop boundary.
func TestAttachPastedImages_MultiTurn_AttachesToLatestUserMessage(t *testing.T) {
	provider, err := NewSproutProvider(nil, &visionMockClient{
		MockClient{model: "vision-model"},
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	sp := provider.(*sproutProvider)

	sp.RegisterPastedImages(map[string][]api.ImageData{
		"_current": {{Base64: "dGVzdA==", Type: "image/png"}},
	})

	messages := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "what is in [image: bar.png]?"},
		// A steer injected at a loop boundary after the turn's query.
		{Role: "user", Content: "also check the footer"},
	}

	out := sp.attachPastedImages(messages)
	if len(out[1].Images) != 0 {
		t.Errorf("the historical first user message must not receive the new image, got %d", len(out[1].Images))
	}
	if len(out[3].Images) != 1 {
		t.Fatalf("the turn's user message should carry the image, got %d", len(out[3].Images))
	}
	if len(out[4].Images) != 0 {
		t.Errorf("a steer message must not steal the image, got %d", len(out[4].Images))
	}
}

// TestAttachPastedImages_AttachesForDeclaredNonVisionClient pins the
// optimistic posture: a negative declaration is a config guess, not a verified
// rejection, so the image is attached and attempted on the active model. The
// send path verifies the real answer and remembers it (see
// api.ResolveVisionCapability).
func TestAttachPastedImages_AttachesForDeclaredNonVisionClient(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	provider, err := NewSproutProvider(nil, &MockClient{model: "text-model"})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	sp := provider.(*sproutProvider)

	sp.RegisterPastedImages(map[string][]api.ImageData{
		"_current": {{Base64: "dGVzdA==", Type: "image/png"}},
	})

	messages := []core.Message{{Role: "user", Content: "describe image"}}
	out := sp.attachPastedImages(messages)

	if len(out[0].Images) != 1 {
		t.Errorf("a declared-non-vision model must still be tried with the image, got %d", len(out[0].Images))
	}
}

// TestAttachPastedImages_SkipsAfterVerifiedRejection verifies that a
// runtime-recorded capability rejection withholds images — the only negative
// answer that suppresses them.
func TestAttachPastedImages_SkipsAfterVerifiedRejection(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	provider, err := NewSproutProvider(nil, &MockClient{model: "rejects-images"})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	sp := provider.(*sproutProvider)

	client := sp.currentClient()
	api.RecordVisionAcceptance(client.GetProvider(), client.GetModel(), false)

	sp.RegisterPastedImages(map[string][]api.ImageData{
		"_current": {{Base64: "dGVzdA==", Type: "image/png"}},
	})

	messages := []core.Message{{Role: "user", Content: "describe image"}}
	out := sp.attachPastedImages(messages)

	if len(out[0].Images) != 0 {
		t.Errorf("a verified rejection must withhold images, got %d", len(out[0].Images))
	}
}

// TestAttachPastedImages_LearnedVisionOverridesDeclaration (SP-140 Phase 2):
// a client that declares no vision still receives pasted images once the
// runtime cache records that the provider/model pair accepts them —
// declared-false is no longer a silent drop for models known to see.
func TestAttachPastedImages_LearnedVisionOverridesDeclaration(t *testing.T) {
	t.Setenv("SPROUT_STATE_DIR", t.TempDir())
	provider, err := NewSproutProvider(nil, &MockClient{model: "text-model"})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	sp := provider.(*sproutProvider)

	client := sp.currentClient()
	api.RecordVisionAcceptance(client.GetProvider(), client.GetModel(), true)

	sp.RegisterPastedImages(map[string][]api.ImageData{
		"_current": {{Base64: "dGVzdA==", Type: "image/png"}},
	})

	messages := []core.Message{{Role: "user", Content: "describe image"}}
	out := sp.attachPastedImages(messages)

	if len(out[0].Images) != 1 {
		t.Errorf("learned-vision client should receive images, got %d", len(out[0].Images))
	}
}

// TestSeedQueryRegistersPastedImages is a regression test for the webui
// image-paste bug. seed_query.go must register pasted images with the
// sproutProvider via registerPastedImagesWithProvider; before the fix, the
// pastedImageMap was built but never registered, so attachPastedImages
// (a no-op on an empty map) silently dropped images.
func TestSeedQueryRegistersPastedImages(t *testing.T) {
	cm, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	a := &Agent{
		configManager: cm,
		state:         NewAgentStateManager(false),
		client:        &visionMockClient{MockClient{model: "vision-model"}},
	}

	prov, err := NewSproutProvider(a, a.getClient())
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	sp := prov.(*sproutProvider)

	// Call the real registration helper used by seed_query.go.
	pastedImageMap := map[string][]api.ImageData{
		"_current": {{Base64: "dGVzdA==", Type: "image/png"}},
	}
	registerPastedImagesWithProvider(a, prov, pastedImageMap)

	// Verify the provider now has the images registered and will attach them.
	out := sp.attachPastedImages([]core.Message{{Role: "user", Content: "test"}})
	if len(out[0].Images) != 1 {
		t.Fatalf("expected provider to attach 1 image after registration, got %d", len(out[0].Images))
	}
}

// TestRegisterPastedImagesWithProvider_NoImagesIsNoOp verifies the helper
// does nothing (and makes no type assertion) when there are no images.
func TestRegisterPastedImagesWithProvider_NoImagesIsNoOp(t *testing.T) {
	cm, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}
	a := &Agent{configManager: cm, state: NewAgentStateManager(false)}

	// A non-sproutProvider would panic if the helper asserted type with no
	// guard; pass nil images to confirm the empty short-circuit.
	registerPastedImagesWithProvider(a, nil, nil)
}
