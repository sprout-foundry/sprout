//go:build !js && windows

package cmd

import "os/exec"

// setProcessGroup is a no-op on Windows. Unix uses Setsid to detach the
// child into a new session/process group so it survives the parent
// exiting. Windows has no equivalent of sessions/process groups — child
// processes are inherently independent and don't receive SIGHUP-like
// signals when the parent dies.
func setProcessGroup(cmd *exec.Cmd) {}
