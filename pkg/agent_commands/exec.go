package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

// ExecCommand handles the /exec slash command
// Usage: /exec <shell-command-to-execute>
// Alias: !<shell-command-to-execute> (matches other tools like Jupyter, R, etc.)
type ExecCommand struct{}

func (c *ExecCommand) Name() string {
	return "exec"
}

func (c *ExecCommand) Description() string {
	return "Execute a shell command directly (also use !<command> as shortcut)"
}

func (c *ExecCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: /exec <shell-command-to-execute>")
	}

	command := strings.Join(args, " ")

	// Execute the shell command using the same pattern as direct shell execution
	fmt.Printf("\033[34m[shell]\033[0m Executing: %s\n", command)
	result, err := tools.ExecuteShellCommand(context.Background(), command)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return nil
	}

	fmt.Printf("----------------------------\n")
	fmt.Print(result)
	if !strings.HasSuffix(result, "\n") {
		fmt.Print("\n")
	}
	fmt.Printf("----------------------------\n")

	return nil
}

// IsShellCommand checks if a prompt starts with common shell tools
func IsShellCommand(prompt string) bool {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return false
	}

	lower := strings.ToLower(trimmed)

	// Common shell command prefixes
	shellPrefixes := []string{
		"ls", "ll", "la", "dir", "pwd", "cd", "cat", "less", "more", "head", "tail",
		"grep", "find", "echo", "clear", "history", "man", "which", "whoami",
		"ps", "top", "htop", "df", "du", "free", "uptime", "date", "cal",
		"cp", "mv", "rm", "mkdir", "rmdir", "touch", "chmod", "chown",
		"git", "go", "npm", "yarn", "python", "python3", "pip", "pip3",
		"docker", "kubectl", "make", "cargo", "rustc", "node", "deno",
		"vim", "vi", "nano", "emacs", "code", "subl", "tree", "wc",
		"curl", "wget", "ping", "netstat", "ss", "ip", "ifconfig",
		"brew", "apt", "apt-get", "yum", "snap", "flatpak",
		"sed", "awk", "cut", "sort", "uniq", "tr", "tee", "xargs",
		"kill", "killall", "pkill", "jobs", "fg", "bg", "nohup",
		"export", "source", "alias", "unalias", "type", "env",
		"tar", "gzip", "gunzip", "zip", "unzip", "bzip2", "bunzip2",
		"sha1sum", "sha256sum", "md5sum", "openssl", "base64",
		"systemctl", "service", "journalctl", "dmesg", "lsof", "strace",
		"diff", "comm", "paste", "join", "split", "csplit",
		"test", "true", "false", "yes", "seq", "expr", "bc",
		"screen", "tmux", "watch", "time", "timeout", "sleep",
		"mount", "umount", "fdisk", "mkfs", "fsck", "blkid",
		"id", "groups", "users", "who", "w", "last", "su", "sudo",
		"ssh", "scp", "rsync", "ftp", "sftp", "telnet", "nc", "nmap",
		"iptables", "ufw", "firewall-cmd", "tcpdump", "wireshark",
		"locate", "updatedb", "whereis", "file", "stat", "ln",
		"crontab", "at", "batch", "nohup", "nice", "renice",
		"patch", "diff", "git", "svn", "hg", "cvs",
		"gcc", "g++", "clang", "javac", "rustc", "go",
		"mysql", "psql", "sqlite3", "redis-cli", "mongo",
		"jq", "yq", "xmllint", "tig", "ag", "rg", "fd",
		"lspci", "lsusb", "lsblk", "lscpu", "lshw", "dmidecode",
		"modprobe", "lsmod", "rmmod", "insmod", "depmod",
		"hostnamectl", "timedatectl", "localectl", "loginctl",
	}

	for _, prefix := range shellPrefixes {
		if strings.HasPrefix(lower, prefix+" ") || lower == prefix {
			return true
		}
	}
	return false
}

// ExecuteShellCommandDirectly executes a shell command directly and returns the result
func ExecuteShellCommandDirectly(command string) (string, error) {
	return tools.ExecuteShellCommand(context.Background(), command)
}
