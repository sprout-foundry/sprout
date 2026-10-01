package filesystem

// filesystem_sensitive.go — the sensitive-system-path detection: the
// per-vendor sensitive path lists (aws, gpg, ssh, kube, docker, gcloud,
// azure) and IsSensitiveSystemPath / IsHomeDir, split out of filesystem.go.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// sensitiveSystemPaths is the list of system paths that always require
// a user prompt even when session-elevated or allowlisted.
// Matched by prefix — both /etc/passwd and /etc/passwd.lock qualify.
var sensitiveSystemPaths = []string{
	"/etc/passwd",
	"/etc/shadow",
	"/etc/sudoers",
	"/etc/sudoers.d",
	"/root/.ssh",
	"/.ssh",
}

// awsSensitivePaths matches AWS credential locations under $HOME/.aws.
var awsSensitivePaths = []string{
	".aws/credentials",
	".aws/config",
}

// gpgSensitivePaths matches GPG keyring locations under $HOME/.gnupg.
var gpgSensitivePaths = []string{
	".gnupg",
}

// sshConfigSensitivePaths matches SSH config and known-hosts files under $HOME/.ssh.
var sshConfigSensitivePaths = []string{
	".ssh/config",
	".ssh/known_hosts",
	".ssh/authorized_keys",
	".ssh/known_hosts.old",
}

// kubeSensitivePaths matches Kubernetes config under $HOME/.kube.
var kubeSensitivePaths = []string{
	".kube/config",
	".kube/config.backup",
}

// dockerSensitivePaths matches Docker config under $HOME/.docker.
var dockerSensitivePaths = []string{
	".docker/config.json",
}

// gcloudSensitivePaths matches Google Cloud config under $HOME/.config/gcloud.
var gcloudSensitivePaths = []string{
	".config/gcloud",
}

// azureSensitivePaths matches Azure config under $HOME/.azure.
var azureSensitivePaths = []string{
	".azure",
}

// IsSensitiveSystemPath reports whether path targets a known sensitive system
// location that should always prompt the user rather than auto-allowing.
// Covers: /etc/* passwd/shadow/sudoers, SSH private keys and config, AWS
// credentials, GPG keyrings, Kubernetes, Docker, GCP, and Azure configs.
func IsSensitiveSystemPath(path string) bool {
	cleanPath := filepath.Clean(path)

	// /etc system files
	for _, p := range sensitiveSystemPaths {
		if strings.HasPrefix(cleanPath, p) {
			return true
		}
	}

	// SSH private keys: /root/.ssh/id_*, /home/*/.ssh/id_*, ~/.ssh/id_*
	// We use home dir expansion for the ~/ variant.
	if strings.HasPrefix(cleanPath, "/.ssh") || strings.HasPrefix(cleanPath, "/root/.ssh") {
		base := filepath.Base(cleanPath)
		if strings.HasPrefix(base, "id_") {
			return true
		}
	}
	// Check /home/*/.ssh pattern
	if strings.HasPrefix(cleanPath, "/home/") {
		rest := strings.TrimPrefix(cleanPath, "/home/")
		if idx := strings.Index(rest, "/"); idx >= 0 {
			afterUser := rest[idx+1:]
			if strings.HasPrefix(afterUser, ".ssh/") {
				base := filepath.Base(cleanPath)
				if strings.HasPrefix(base, "id_") {
					return true
				}
			}
		}
	}
	// ~/... variants via home dir
	if home, err := os.UserHomeDir(); err == nil {
		rel, _ := filepath.Rel(home, cleanPath)
		if !strings.HasPrefix(rel, "..") {
			// Path is under $HOME
			for _, p := range awsSensitivePaths {
				if strings.HasPrefix(filepath.ToSlash(rel), p) || rel == p {
					return true
				}
			}
			if strings.HasPrefix(filepath.ToSlash(rel), ".ssh/") {
				base := filepath.Base(cleanPath)
				if strings.HasPrefix(base, "id_") {
					return true
				}
			}
			if strings.HasPrefix(filepath.ToSlash(rel), ".gnupg") {
				return true
			}
			// SSH config, known_hosts, authorized_keys
			for _, p := range sshConfigSensitivePaths {
				if strings.HasPrefix(filepath.ToSlash(rel), p) || rel == p {
					return true
				}
			}
			// Kubernetes config
			for _, p := range kubeSensitivePaths {
				if strings.HasPrefix(filepath.ToSlash(rel), p) || rel == p {
					return true
				}
			}
			// Docker config
			for _, p := range dockerSensitivePaths {
				if strings.HasPrefix(filepath.ToSlash(rel), p) || rel == p {
					return true
				}
			}
			// Google Cloud config
			for _, p := range gcloudSensitivePaths {
				if strings.HasPrefix(filepath.ToSlash(rel), p) || rel == p {
					return true
				}
			}
			// Azure config
			for _, p := range azureSensitivePaths {
				if strings.HasPrefix(filepath.ToSlash(rel), p) || rel == p {
					return true
				}
			}
		}
	}

	return false
}

// IsHomeDir reports whether path is the current user's home directory.
// Both paths are resolved through symlinks so that, e.g., /var/folders/...
// and /Users/deva compare correctly on macOS.
//
// Symlink resolution is bounded by symlinkTimeout so a hanging network mount
// (NFS, SMB) cannot stall index builds indefinitely.
func IsHomeDir(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), symlinkTimeout)
	defer cancel()
	resolved, err := evalSymlinksWithTimeout(ctx, filepath.Clean(path))
	if err != nil {
		resolved = filepath.Clean(path)
	}
	homeResolved, err := evalSymlinksWithTimeout(ctx, home)
	if err != nil {
		homeResolved = home
	}
	return resolved == homeResolved
}
