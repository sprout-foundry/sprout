package api

import "testing"

// resolverTestClient is the minimal client surface the resolver reads.
type resolverTestClient struct {
	ClientInterface
	provider  string
	model     string
	vision    bool
	selfHeals bool
}

func (c *resolverTestClient) GetProvider() string { return c.provider }
func (c *resolverTestClient) GetModel() string    { return c.model }
func (c *resolverTestClient) SupportsVision() bool {
	return c.vision
}
func (c *resolverTestClient) RecoversFromVisionRejection() bool {
	return c.selfHeals
}
func (c *resolverTestClient) VisionCapabilities() VisionCapabilities {
	return VisionCapabilities{}
}

// structuralTestClient reports a transport that cannot carry image parts.
type structuralTestClient struct {
	resolverTestClient
}

func (c *structuralTestClient) CannotAcceptImages() bool { return true }

func TestResolveVisionCapability_OptimisticDefault(t *testing.T) {
	visionLearnedTestEnv(t)

	// No learned observation, not in the registry catalog (probe nil), the
	// client declares no vision, but it can recover from a rejection on the
	// wire: the image is still attempted.
	c := &resolverTestClient{provider: "resolver-test", model: "declared-no", vision: false, selfHeals: true}
	capability := ResolveVisionCapability(c)
	if !capability.AcceptsImages {
		t.Fatal("declared=false with no verified rejection must still try the image")
	}
	if capability.Source != "optimistic" {
		t.Fatalf("source = %q, want optimistic", capability.Source)
	}

	// A positive declaration is the recorded basis, same answer.
	c = &resolverTestClient{provider: "resolver-test", model: "declared-yes", vision: true}
	capability = ResolveVisionCapability(c)
	if !capability.AcceptsImages || capability.Source != "declared" {
		t.Fatalf("declared=true: got accepts=%v source=%q, want true/declared", capability.AcceptsImages, capability.Source)
	}
}

func TestResolveVisionCapability_NoRecoveryKeepsDeclaration(t *testing.T) {
	visionLearnedTestEnv(t)

	// A client with no verify-and-remember path keeps its declared answer:
	// sending it an image it says it cannot take would hard-fail the turn.
	c := &resolverTestClient{provider: "resolver-test", model: "declared-no", vision: false}
	capability := ResolveVisionCapability(c)
	if capability.AcceptsImages {
		t.Fatal("a client that cannot recover from a rejection must not get an optimistic image")
	}
	if capability.Source != "declared" {
		t.Fatalf("source = %q, want declared", capability.Source)
	}
}

func TestResolveVisionCapability_LearnedRejectionWins(t *testing.T) {
	visionLearnedTestEnv(t)

	c := &resolverTestClient{provider: "resolver-test", model: "rejects-images", vision: true}
	RecordVisionAcceptance(c.provider, c.model, false)

	capability := ResolveVisionCapability(c)
	if capability.AcceptsImages {
		t.Fatal("a runtime-verified rejection must withhold images")
	}
	if capability.Source != "runtime" {
		t.Fatalf("source = %q, want runtime", capability.Source)
	}

	// A later verified success clears the veto.
	RecordVisionAcceptance(c.provider, c.model, true)
	capability = ResolveVisionCapability(c)
	if !capability.AcceptsImages {
		t.Fatal("a runtime-verified success must restore images")
	}
}

func TestResolveVisionCapability_StructuralVeto(t *testing.T) {
	visionLearnedTestEnv(t)

	c := &structuralTestClient{resolverTestClient{provider: "resolver-test", model: "text-only-engine", vision: true}}
	capability := ResolveVisionCapability(c)
	if capability.AcceptsImages {
		t.Fatal("a structural veto must withhold images even when vision is declared")
	}
	if capability.Source != "structural" {
		t.Fatalf("source = %q, want structural", capability.Source)
	}
}

func TestResolveVisionCapability_NilClient(t *testing.T) {
	if capability := ResolveVisionCapability(nil); capability.AcceptsImages {
		t.Fatal("nil client should resolve to zero capability")
	}
}
