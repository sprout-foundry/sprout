//go:build windows && !js

package automate

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils/console"
)

const sleeperEnv = "SPROUT_AUTOMATE_TEST_SLEEPER"

func TestHelperSleeperProcess(t *testing.T) {
	if os.Getenv(sleeperEnv) != "1" {
		t.Skip("helper process for stop_process_windows_test.go")
	}
	time.Sleep(time.Minute)
}

func startSleeper(t *testing.T, newGroup bool) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSleeperProcess$") //nolint:gosec // G204: re-executes this test binary as a sleeper
	cmd.Env = append(os.Environ(), sleeperEnv+"=1")
	if newGroup {
		console.SetNewProcessGroup(cmd)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})
	return cmd
}

func TestIsProcessGroupLeader(t *testing.T) {
	leader := startSleeper(t, true)
	member := startSleeper(t, false)

	if !isProcessGroupLeader(leader.Process.Pid) {
		t.Errorf("child started with CREATE_NEW_PROCESS_GROUP should lead its group")
	}
	if isProcessGroupLeader(member.Process.Pid) {
		t.Errorf("child sharing our process group must not be treated as a group leader")
	}
	if canCtrlBreak(member.Process.Pid) {
		t.Errorf("CTRL_BREAK to a non-leader would broadcast to our whole console")
	}
}

func TestStopProcess_NonGroupLeaderIsTerminated(t *testing.T) {
	member := startSleeper(t, false)
	pid := member.Process.Pid

	ok, err := StopProcess(pid)
	if err != nil {
		t.Fatalf("StopProcess: %v", err)
	}
	if !ok {
		t.Fatalf("StopProcess(%d) reported the process still alive", pid)
	}
}

func TestStopProcess_GroupLeader(t *testing.T) {
	leader := startSleeper(t, true)
	pid := leader.Process.Pid

	ok, err := StopProcess(pid)
	if err != nil {
		t.Fatalf("StopProcess: %v", err)
	}
	if !ok {
		t.Fatalf("StopProcess(%d) reported the process still alive", pid)
	}
}

func TestProcessStartedBefore_Windows(t *testing.T) {
	before := time.Now().Add(-time.Second)
	child := startSleeper(t, false)
	pid := child.Process.Pid

	if !processStartedBefore(pid, time.Now().Add(time.Second)) {
		t.Errorf("child started before now+1s")
	}
	if processStartedBefore(pid, before) {
		t.Errorf("child started after %v; a recycled PID must be detected", before)
	}
}
