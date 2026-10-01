//go:build linux

package service

// linux_port.go — port/PID discovery helpers for the Linux service
// manager: isPortInUse, findPIDOnPort, and the fuser/pgrep/proc-net
// discovery paths. Split out of linux.go.
import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// isPortInUse checks if a TCP port is already in use by attempting to bind to it.
func isPortInUse(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	listener.Close()
	return false
}

// findPIDOnPort returns the PID of a process listening on the given port.
// Tries 'fuser' first (works on most Linux); falls back to /proc/net/tcp;
// then pgrep for the known sprout daemon process name.
func findPIDOnPort(port int) int {
	if pid := findPIDViaFuser(port); pid > 0 {
		return pid
	}
	if pid := findPIDViaProcNet(port); pid > 0 {
		return pid
	}
	return findPIDViaPgrep()
}

// findPIDViaFuser runs 'fuser <port>/tcp' to find the PID holding a port.
func findPIDViaFuser(port int) int {
	cmd := exec.Command("fuser", fmt.Sprintf("%d/tcp", port))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	for _, f := range fields {
		// fuser outputs PIDs with a '+' for listening sockets; strip it
		pidStr := strings.TrimRight(f, "+")
		if pid, err := strconv.Atoi(pidStr); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// findPIDViaPgrep runs 'pgrep -f "sprout agent"' as a last resort for
// environments where fuser and /proc/net/tcp are both inaccessible (e.g.
// Termux sandbox). Returns the first matching PID or 0.
func findPIDViaPgrep() int {
	cmd := exec.Command("pgrep", "-f", "sprout agent")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0
	}
	for _, line := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(line); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// findPIDViaProcNet scans /proc/net/tcp for a listening socket on the port,
// then finds the owning PID via /proc/[pid]/fd inode matching.
func findPIDViaProcNet(port int) int {
	data, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return 0
	}
	hexPort := fmt.Sprintf("%04X", port)
	target := fmt.Sprintf("0100007F:%s", hexPort)
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, " sl ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		if fields[1] == target && strings.TrimSpace(fields[3]) == "0A" {
			inode := strings.TrimSpace(fields[9])
			return findPIDByInode(inode)
		}
	}
	return 0
}

// findPIDByInode scans /proc/[pid]/fd for a socket with the given inode.
// Only matches processes owned by the current user.
func findPIDByInode(inode string) int {
	myUID := os.Getuid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		// Skip processes not owned by us
		status, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if !strings.Contains(string(status), fmt.Sprintf("Uid:\t%d", myUID)) {
			continue
		}
		fds, _ := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
		for _, fd := range fds {
			link, _ := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, fd.Name()))
			if strings.Contains(link, "socket:"+inode) {
				return pid
			}
		}
	}
	return 0
}
