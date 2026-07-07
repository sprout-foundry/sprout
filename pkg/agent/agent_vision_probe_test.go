package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// boolPtr returns a pointer to b for test setup.
var _ = boolPtr // guard against duplicate: boolPtr is defined in change_tracking_config_gate_test.go

func TestEffectiveVisionSupport_ProbeTrue(t *testing.T) {
	a := &Agent{}
	a.setClient(&visionProbeTestClient{
		model:          "test-model",
		supportsVision: false, // config says no vision
	}, "test")
	a.visionProbeModel = "test-model"
	a.visionProbeProvider = "test"
	a.visionProbeResult = boolPtr(true) // probe says yes
	if !a.effectiveVisionSupport() {
		t.Error("probe=true should override config=false")
	}
}

func TestEffectiveVisionSupport_ProbeFalse(t *testing.T) {
	a := &Agent{}
	a.setClient(&visionProbeTestClient{
		model:          "test-model",
		supportsVision: true, // config says vision
	}, "test")
	a.visionProbeModel = "test-model"
	a.visionProbeProvider = "test"
	a.visionProbeResult = boolPtr(false) // probe says no
	if a.effectiveVisionSupport() {
		t.Error("probe=false should override config=true")
	}
}

func TestEffectiveVisionSupport_NoProbe_FallsBackToConfig(t *testing.T) {
	a := &Agent{}
	a.setClient(&visionProbeTestClient{
		model:          "test-model",
		supportsVision: true,
	}, "test")
	// visionProbeResult is nil — never probed
	if !a.effectiveVisionSupport() {
		t.Error("nil probe should fall back to config=true")
	}

	a2 := &Agent{}
	a2.setClient(&visionProbeTestClient{
		model:          "test-model",
		supportsVision: false,
	}, "test")
	if a2.effectiveVisionSupport() {
		t.Error("nil probe should fall back to config=false")
	}
}

func TestEffectiveVisionSupport_NilClient(t *testing.T) {
	a := &Agent{}
	if a.effectiveVisionSupport() {
		t.Error("nil client should return false")
	}
}

// visionProbeTestClient is a minimal ClientInterface for probe-vision tests.
// It only implements what effectiveVisionSupport touches.
type visionProbeTestClient struct {
	api.ClientInterface
	model          string
	supportsVision bool
}

func (c *visionProbeTestClient) GetModel() string                   { return c.model }
func (c *visionProbeTestClient) SupportsVision() bool               { return c.supportsVision }
func (c *visionProbeTestClient) SupportsConversationalVision() bool { return c.supportsVision }
