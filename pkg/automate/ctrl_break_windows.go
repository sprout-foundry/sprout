//go:build windows && !js

package automate

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// canCtrlBreak reports whether CTRL_BREAK_EVENT addressed to pid reaches
// only pid's process group. GenerateConsoleCtrlEvent broadcasts to every
// process on our console (sprout included) when pid does not lead a
// process group on it, so the event is only safe for a group leader that
// shares our console. Anything else is stopped with TerminateProcess.
func canCtrlBreak(pid int) bool {
	return attachedToOurConsole(pid) && isProcessGroupLeader(pid)
}

func attachedToOurConsole(pid int) bool {
	ids := make([]uint32, 64)
	for {
		n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids))) //nolint:gosec // G103: audited PEB/console introspection
		if n == 0 {
			return false
		}
		if int(n) > len(ids) {
			ids = make([]uint32, n)
			continue
		}
		for _, id := range ids[:n] {
			if int(id) == pid {
				return true
			}
		}
		return false
	}
}

// isProcessGroupLeader reads the console process group ID from the
// target's PEB; there is no documented API that exposes it.
func isProcessGroupLeader(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid)) //nolint:gosec // G115: Windows PIDs fit in uint32
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var pbi windows.PROCESS_BASIC_INFORMATION
	if err := windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation,
		unsafe.Pointer(&pbi), uint32(unsafe.Sizeof(pbi)), nil); err != nil { //nolint:gosec // G103: audited PEB introspection
		return false
	}
	var params uintptr
	pebParams := uintptr(unsafe.Pointer(pbi.PebBaseAddress)) + unsafe.Offsetof(windows.PEB{}.ProcessParameters) //nolint:gosec // G103: audited PEB introspection
	if !readRemote(h, pebParams, unsafe.Pointer(&params), unsafe.Sizeof(params)) || params == 0 {               //nolint:gosec // G103: audited PEB introspection
		return false
	}
	var groupID uint32
	groupField := params + unsafe.Offsetof(windows.RTL_USER_PROCESS_PARAMETERS{}.ProcessGroupId)
	if !readRemote(h, groupField, unsafe.Pointer(&groupID), unsafe.Sizeof(groupID)) { //nolint:gosec // G103: audited PEB introspection
		return false
	}
	return groupID == uint32(pid) //nolint:gosec // G115: Windows PIDs fit in uint32
}

func readRemote(h windows.Handle, addr uintptr, dst unsafe.Pointer, size uintptr) bool {
	var n uintptr
	err := windows.ReadProcessMemory(h, addr, (*byte)(dst), size, &n)
	return err == nil && n == size
}
