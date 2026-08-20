package agent

import (
	"os"
	"os/exec"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestForceSaveAndExitPersistsState(t *testing.T) {
	if os.Getenv("SPROUT_TEST_FORCE_SAVE_CHILD") == "1" {
		stateDir := os.Getenv("SPROUT_TEST_STATE_DIR")
		workingDir := os.Getenv("SPROUT_TEST_WORKING_DIR")
		orig := getStateDirFunc
		getStateDirFunc = func() (string, error) { return stateDir, nil }
		defer func() { getStateDirFunc = orig }()

		a := &Agent{state: NewAgentStateManager(false)}
		a.SetSessionID("forcesave")
		a.SetWorkspaceRoot(workingDir)
		a.state.SetMessages([]api.Message{{Role: "user", Content: "saved on force quit"}})
		a.ForceSaveAndExit(3)
		return
	}

	stateDir, workingDir := setupScopedStateTest(t)

	bin, err := os.Executable()
	if err != nil {
		t.Skip("cannot locate test binary")
	}
	cmd := exec.Command(bin, "-test.run=TestForceSaveAndExitPersistsState")
	cmd.Env = append(os.Environ(),
		"SPROUT_TEST_FORCE_SAVE_CHILD=1",
		"SPROUT_TEST_STATE_DIR="+stateDir,
		"SPROUT_TEST_WORKING_DIR="+workingDir,
	)
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != 3 {
			t.Fatalf("child process failed: %v", err)
		}
	}

	state, err := LoadStateWithoutAgentScoped("forcesave", workingDir)
	if err != nil {
		t.Fatalf("state should have been saved before exit: %v", err)
	}
	if len(state.Messages) != 1 || state.Messages[0].Content != "saved on force quit" {
		t.Fatalf("unexpected saved messages: %+v", state.Messages)
	}
}
