package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

func TestVisionCacheInvalidatedOnSetClient(t *testing.T) {
	a := &Agent{}
	a.setClient(&visionProbeTestClient{supportsVision: true}, api.TestClientType)
	a.visionProc = &tools.VisionProcessor{}

	a.setClient(&visionProbeTestClient{supportsVision: false}, api.TestClientType)

	if a.visionProc != nil {
		t.Fatal("vision processor should be cleared when the client changes")
	}
}

func TestVisionProbeFieldsClearedOnSetClient(t *testing.T) {
	a := &Agent{}
	a.visionProbeModel = "old-model"
	a.visionProbeProvider = "old-provider"
	probeResult := true
	a.visionProbeResult = &probeResult

	a.setClient(&visionProbeTestClient{}, api.TestClientType)

	if a.visionProbeModel != "" {
		t.Errorf("vision probe model = %q, want empty", a.visionProbeModel)
	}
	if a.visionProbeProvider != "" {
		t.Errorf("vision probe provider = %q, want empty", a.visionProbeProvider)
	}
	if a.visionProbeResult != nil {
		t.Errorf("vision probe result = %v, want nil", *a.visionProbeResult)
	}
}
