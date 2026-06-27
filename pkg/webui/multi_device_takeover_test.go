//go:build !js

package webui

import (
	"fmt"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// ActiveSessionRegistry — multi-device single-active-session (SP-046-5)
// ---------------------------------------------------------------------------

func TestMultiDevice_SingleDevice(t *testing.T) {
	reg := NewActiveSessionRegistry()

	// First device connects — no takeover prompt.
	prompt, existing := reg.RegisterConnection("S", "deviceA")
	if prompt {
		t.Errorf("first registration should not prompt for takeover, got prompt=true")
	}
	if existing != "" {
		t.Errorf("first registration should have no existing device, got %q", existing)
	}

	// Active device is A.
	if got := reg.GetActiveDevice("S"); got != "deviceA" {
		t.Errorf("GetActiveDevice(S) = %q, want %q", got, "deviceA")
	}

	// Same device reconnects — idempotent, no takeover prompt.
	prompt, existing = reg.RegisterConnection("S", "deviceA")
	if prompt {
		t.Errorf("idempotent re-registration should not prompt, got prompt=true")
	}
	if existing != "" {
		t.Errorf("idempotent re-registration should have no existing device, got %q", existing)
	}
	if got := reg.GetActiveDevice("S"); got != "deviceA" {
		t.Errorf("after idempotent re-register, GetActiveDevice = %q, want %q", got, "deviceA")
	}
}

func TestMultiDevice_TwoDevices(t *testing.T) {
	reg := NewActiveSessionRegistry()

	// Device A connects first — no takeover prompt.
	prompt, existing := reg.RegisterConnection("S", "deviceA")
	if prompt {
		t.Errorf("first registration should not prompt, got prompt=true")
	}
	if existing != "" {
		t.Errorf("first registration should have no existing device, got %q", existing)
	}

	// Device B connects — should trigger takeover prompt.
	prompt, existing = reg.RegisterConnection("S", "deviceB")
	if !prompt {
		t.Error("second device should trigger takeover prompt, got prompt=false")
	}
	if existing != "deviceA" {
		t.Errorf("existing device should be deviceA, got %q", existing)
	}

	// Active device is still A — takeover hasn't been accepted yet.
	if got := reg.GetActiveDevice("S"); got != "deviceA" {
		t.Errorf("before takeover, GetActiveDevice = %q, want %q", got, "deviceA")
	}
}

func TestTakeover_AcceptingTakeover(t *testing.T) {
	reg := NewActiveSessionRegistry()

	// Device A is active.
	reg.RegisterConnection("S", "deviceA")

	// Device B arrives — triggers takeover prompt.
	prompt, existing := reg.RegisterConnection("S", "deviceB")
	if !prompt || existing != "deviceA" {
		t.Errorf("expected takeover prompt for deviceB with existing=deviceA, got prompt=%v, existing=%q", prompt, existing)
	}

	// Accept the takeover — swap B in.
	old := reg.RequestTakeover("S", "deviceB")
	if old != "deviceA" {
		t.Errorf("RequestTakeover should return deviceA, got %q", old)
	}

	// Now B is active.
	if got := reg.GetActiveDevice("S"); got != "deviceB" {
		t.Errorf("after takeover, GetActiveDevice = %q, want %q", got, "deviceB")
	}

	// Device A reconnects — should now see B as the active device.
	prompt, existing = reg.RegisterConnection("S", "deviceA")
	if !prompt {
		t.Error("deviceA reconnecting should trigger takeover prompt, got prompt=false")
	}
	if existing != "deviceB" {
		t.Errorf("deviceA should see deviceB as active, got %q", existing)
	}
}

func TestTakeover_IdempotentRegistration(t *testing.T) {
	reg := NewActiveSessionRegistry()

	// Register device A.
	reg.RegisterConnection("S", "deviceA")

	// Register device A again — should be idempotent.
	prompt, existing := reg.RegisterConnection("S", "deviceA")
	if prompt {
		t.Error("idempotent re-registration should not trigger takeover prompt")
	}
	if existing != "" {
		t.Errorf("idempotent re-registration should have no existing device, got %q", existing)
	}

	if got := reg.GetActiveDevice("S"); got != "deviceA" {
		t.Errorf("GetActiveDevice = %q, want %q", got, "deviceA")
	}
}

func TestTakeover_Disconnect(t *testing.T) {
	reg := NewActiveSessionRegistry()

	// Register device A.
	reg.RegisterConnection("S", "deviceA")

	// Disconnect device A — should succeed.
	if ok := reg.DisconnectDevice("S", "deviceA"); !ok {
		t.Error("DisconnectDevice(S, deviceA) should return true")
	}

	// Session should be gone.
	if got := reg.GetActiveDevice("S"); got != "" {
		t.Errorf("after disconnect, GetActiveDevice = %q, want empty", got)
	}

	// Disconnect device A again — should return false (already gone).
	if ok := reg.DisconnectDevice("S", "deviceA"); ok {
		t.Error("DisconnectDevice on already-gone session should return false")
	}

	// Disconnect a wrong device on an empty session — should return false.
	if ok := reg.DisconnectDevice("S", "deviceB"); ok {
		t.Error("DisconnectDevice with wrong device on empty session should return false")
	}
}

func TestTakeover_TakeoverNonexistent(t *testing.T) {
	reg := NewActiveSessionRegistry()

	old := reg.RequestTakeover("nonexistent", "deviceA")
	if old != "" {
		t.Errorf("RequestTakeover on nonexistent session should return empty, got %q", old)
	}
}

func TestTakeover_MultipleSessions(t *testing.T) {
	reg := NewActiveSessionRegistry()

	// Set up two independent sessions.
	reg.RegisterConnection("S1", "deviceA1")
	reg.RegisterConnection("S2", "deviceB1")

	// Device A2 connects to S1 — takeover prompt for S1.
	prompt, existing := reg.RegisterConnection("S1", "deviceA2")
	if !prompt || existing != "deviceA1" {
		t.Errorf("S1 takeover: expected prompt=true, existing=deviceA1, got prompt=%v, existing=%q", prompt, existing)
	}

	// Device B2 connects to S2 — takeover prompt for S2.
	prompt, existing = reg.RegisterConnection("S2", "deviceB2")
	if !prompt || existing != "deviceB1" {
		t.Errorf("S2 takeover: expected prompt=true, existing=deviceB1, got prompt=%v, existing=%q", prompt, existing)
	}

	// S2's takeover should not affect S1.
	if got := reg.GetActiveDevice("S1"); got != "deviceA1" {
		t.Errorf("S2 takeover should not affect S1, GetActiveDevice(S1) = %q, want %q", got, "deviceA1")
	}

	// Execute takeover on S1 only.
	old := reg.RequestTakeover("S1", "deviceA2")
	if old != "deviceA1" {
		t.Errorf("RequestTakeover(S1, deviceA2) = %q, want %q", old, "deviceA1")
	}

	// S1 now has deviceA2.
	if got := reg.GetActiveDevice("S1"); got != "deviceA2" {
		t.Errorf("after S1 takeover, GetActiveDevice(S1) = %q, want %q", got, "deviceA2")
	}

	// S2 is unaffected — still deviceB1.
	if got := reg.GetActiveDevice("S2"); got != "deviceB1" {
		t.Errorf("S2 should still be deviceB1, got %q", got)
	}
}

func TestTakeover_DisconnectReconnect(t *testing.T) {
	reg := NewActiveSessionRegistry()

	// Register device A.
	reg.RegisterConnection("S", "deviceA")

	// Disconnect device A.
	if ok := reg.DisconnectDevice("S", "deviceA"); !ok {
		t.Error("DisconnectDevice(S, deviceA) should return true")
	}

	// Re-register after disconnect — should succeed without prompt.
	prompt, existing := reg.RegisterConnection("S", "deviceA")
	if prompt {
		t.Error("re-registration after disconnect should not prompt")
	}
	if existing != "" {
		t.Errorf("re-registration after disconnect should have no existing, got %q", existing)
	}
	if reg.GetActiveDevice("S") != "deviceA" {
		t.Errorf("re-registration after disconnect should set active device to deviceA, got %q", reg.GetActiveDevice("S"))
	}
}

func TestActiveSessionRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewActiveSessionRegistry()
	var wg sync.WaitGroup
	const iterations = 50

	for i := 0; i < iterations; i++ {
		sid := fmt.Sprintf("S%d", i)
		wg.Add(3)
		go func() {
			defer wg.Done()
			reg.RegisterConnection(sid, "deviceA")
		}()
		go func() {
			defer wg.Done()
			reg.RegisterConnection(sid, "deviceB")
		}()
		go func() {
			defer wg.Done()
			reg.GetActiveDevice(sid)
		}()
	}
	wg.Wait()

	// Verify each session has exactly one active device (not "")
	for i := 0; i < iterations; i++ {
		sid := fmt.Sprintf("S%d", i)
		active := reg.GetActiveDevice(sid)
		if active == "" {
			t.Errorf("session %s has no active device after concurrent registrations", sid)
		}
		if active != "deviceA" && active != "deviceB" {
			t.Errorf("session %s has unexpected active device %q", sid, active)
		}
	}
}
