package semantic

import (
	"testing"
	"time"
)

func TestTypeScriptSessionAdapterHealthyWhenFresh(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	// No process started, so Healthy should be false
	if a.Healthy() {
		t.Error("fresh adapter with no process should not be healthy (no cmd)")
	}
}

func TestTypeScriptSessionAdapterClose(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	if err := a.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !a.closed {
		t.Error("expected closed to be true")
	}
}

func TestTypeScriptSessionAdapterRunWhenClosed(t *testing.T) {
	a := &typeScriptSessionAdapter{}
	a.Close()
	_, err := a.Run(ToolInput{Method: "diagnostics"})
	if err == nil {
		t.Error("expected error when running on closed adapter")
	}
}

func TestNewTypeScriptSessionPool(t *testing.T) {
	pool := NewTypeScriptSessionPool(5 * time.Minute)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	pool.Close()
}
