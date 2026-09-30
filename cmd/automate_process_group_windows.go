//go:build !js && windows

package cmd

import (
	"os"
	"os/exec"

	"github.com/sprout-foundry/sprout/pkg/utils/console"
)

// setProcessGroup starts the child in its own console process group so
// automate.StopProcess can deliver CTRL_BREAK_EVENT to it alone; without
// a group of its own the event would reach every process on our console.
func setProcessGroup(cmd *exec.Cmd) {
	console.SetNewProcessGroup(cmd)
}

// forwardSignal delivers CTRL_BREAK_EVENT to the child's process group:
// Windows cannot send os.Interrupt to another process, and the child's own
// group never sees our console's Ctrl-C. Go surfaces the event to the child
// as os.Interrupt, so its graceful-shutdown path runs as on Unix.
func forwardSignal(cmd *exec.Cmd, _ os.Signal) error {
	return console.SendCtrlBreak(cmd.Process.Pid)
}
