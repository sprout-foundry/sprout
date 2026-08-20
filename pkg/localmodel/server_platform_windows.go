//go:build windows

package localmodel

import "syscall"

// detachedSysProcAttr returns platform-specific process attributes to
// detach the server from the parent's process group so it survives CLI exit.
func detachedSysProcAttr() *syscall.SysProcAttr {
	// CREATE_NEW_PROCESS_GROUP (0x200) lets the child run in its own
	// process group so Ctrl+C on the parent doesn't kill it.
	return &syscall.SysProcAttr{CreationFlags: 0x00000200}
}
