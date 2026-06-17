package configuration

import "testing"

func TestEditApprovalConfig_DefaultOff(t *testing.T) {
	cfg := &EditApprovalConfig{}
	if cfg.Resolve().Mode != "off" {
		t.Error("expected default mode 'off'")
	}
}

func TestEditApprovalConfig_NilSafe(t *testing.T) {
	var cfg *EditApprovalConfig
	if cfg.ShouldGate("foo.go") {
		t.Error("nil config should not gate")
	}
}

func TestEditApprovalConfig_OffMode(t *testing.T) {
	cfg := &EditApprovalConfig{Mode: "off"}
	if cfg.ShouldGate("foo.go") {
		t.Error("off mode should never gate")
	}
}

func TestEditApprovalConfig_AllMode(t *testing.T) {
	cfg := &EditApprovalConfig{Mode: "all"}
	if !cfg.ShouldGate("foo.go") {
		t.Error("all mode should gate every path")
	}
}

func TestEditApprovalConfig_PathsMode(t *testing.T) {
	cfg := &EditApprovalConfig{Mode: "paths", Paths: []string{"*.go"}}
	if !cfg.ShouldGate("main.go") {
		t.Error("expected *.go to be gated")
	}
	if cfg.ShouldGate("app.ts") {
		t.Error("did not expect app.ts to be gated")
	}
}
