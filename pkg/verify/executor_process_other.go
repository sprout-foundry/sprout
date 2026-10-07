//go:build js || windows

package verify

import "os/exec"

// bindProcessGroup keeps exec's default cancellation (kill the process);
// Run's WaitDelay bounds any descendant that still holds the output pipe.
func bindProcessGroup(*exec.Cmd) {}
