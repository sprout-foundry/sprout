//go:build !js

package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

func TestExitCodeFromWaitErr_NilIsZero(t *testing.T) {
	if got := exitCodeFromWaitErr(nil); got != 0 {
		t.Errorf("exitCodeFromWaitErr(nil) = %d, want 0", got)
	}
}

func TestExitCodeFromWaitErr_ExitErrorCarriesCode(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 3").Run()
	if got := exitCodeFromWaitErr(err); got != 3 {
		t.Errorf("exitCodeFromWaitErr(exit 3) = %d, want 3 (err: %v)", got, err)
	}
}

func TestExitCodeFromWaitErr_SignalDeathIsNegative(t *testing.T) {
	err := exec.Command("sh", "-c", "kill -9 $$").Run()
	if got := exitCodeFromWaitErr(err); got != -1 {
		t.Errorf("exitCodeFromWaitErr(SIGKILL) = %d, want -1 (err: %v)", got, err)
	}
}

func TestExitCodeFromWaitErr_WrappedAndOtherErrors(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 137").Run()
	wrapped := fmt.Errorf("workflow failed: %w", err)
	if got := exitCodeFromWaitErr(wrapped); got != 137 {
		t.Errorf("wrapped exit 137 → %d, want 137 (err: %v)", got, err)
	}
	if got := exitCodeFromWaitErr(errors.New("wait: no child processes")); got != -1 {
		t.Errorf("non-exit error → %d, want -1", got)
	}
}
