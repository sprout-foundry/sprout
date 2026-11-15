package runner

import (
	"encoding/json"
	"testing"
)

// The heartbeat's capabilities go on the wire as the protocol's object
// (platform docs/runners/PROTOCOL.md §4), not a list of names: the platform
// decodes that shape, so a list would fail its heartbeat with 400.
func TestHeartbeatCapabilitiesAreAnObjectOnTheWire(t *testing.T) {
	b, err := json.Marshal(Heartbeat{Status: "online", Capabilities: &Capabilities{Gateway: true, TaskErrors: true}})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	var caps map[string]bool
	if err := json.Unmarshal(wire["capabilities"], &caps); err != nil {
		t.Fatalf("capabilities = %s, want an object: %v", wire["capabilities"], err)
	}
	if !caps["gateway"] || !caps["task_errors"] || caps["local_dirs"] {
		t.Fatalf("capabilities = %v", caps)
	}
}
