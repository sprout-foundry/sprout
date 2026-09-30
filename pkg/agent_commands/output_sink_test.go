package commands

import "testing"

// Commands the WebUI can run (SafeDuringSteer) have their output captured
// through the registry writer; one that prints to os.Stdout shows nothing
// in the browser.
func TestSteerCapableCommandsWriteThroughRegistryOutput(t *testing.T) {
	r := NewCommandRegistry()
	for name, cmd := range r.commands {
		sc, ok := cmd.(SteerCapable)
		if !ok || !sc.SafeDuringSteer() {
			continue
		}
		if _, ok := cmd.(OutputCommand); !ok {
			t.Errorf("/%s (%T) runs in the WebUI but does not implement OutputCommand — embed outputSink and print via out()", name, cmd)
		}
	}
}
