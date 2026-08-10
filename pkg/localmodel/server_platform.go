//go:build darwin || linux

package localmodel

import "syscall"

// detachedSysProcAttr returns platform-specific process attributes to
// detach the server from the parent's process group so it survives CLI exit.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
