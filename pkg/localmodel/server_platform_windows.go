//go:build windows

package localmodel

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detachedSysProcAttr gives the server its own hidden console and process
// group so it survives CLI exit: a child sharing the CLI's console dies
// when that terminal closes or receives Ctrl+C/Ctrl+Break.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW}
}
