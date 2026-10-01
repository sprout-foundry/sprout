//go:build windows && !js

package pidalive

import (
	"os"
	"os/exec"
	"testing"
)

func TestIsAliveWindows(t *testing.T) {
	if !IsAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
	if IsAlive(0) || IsAlive(-1) {
		t.Error("non-positive PIDs must not be alive")
	}
	if IsAlive(999999999) {
		t.Error("nonexistent PID must not be alive")
	}
	if !IsAlive(4) {
		t.Error("the System process (PID 4) is always running; access denied must still read as alive")
	}

	cmd := exec.Command("cmd.exe", "/d", "/c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait child: %v", err)
	}
	if IsAlive(pid) {
		t.Errorf("exited child %d should not be alive", pid)
	}
}
