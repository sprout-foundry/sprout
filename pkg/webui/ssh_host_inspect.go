package webui

import (
	"context"
	"fmt"
	"strings"
)

func inspectRemoteSSHHost(ctx context.Context, hostAlias string, logger *sshLaunchLogger) (*remoteSSHInfo, error) {
	cmd := newSSHCommandContext(ctx, hostAlias, "uname -s; uname -m")
	out, err := runSSHLoggedCommand(logger, "inspect-remote", fmt.Sprintf("ssh %s uname -s; uname -m", hostAlias), cmd)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("failed to inspect remote host %s", hostAlias)
	}

	platform := normalizeRemotePlatform(lines[0])
	if platform == "" {
		return nil, fmt.Errorf("remote host %s is %s; only Linux and macOS SSH targets are supported", hostAlias, strings.TrimSpace(lines[0]))
	}
	arch := normalizeRemoteArch(lines[1])
	if arch == "" {
		return nil, fmt.Errorf("unsupported remote architecture: %s", strings.TrimSpace(lines[1]))
	}

	return &remoteSSHInfo{Platform: platform, Arch: arch}, nil
}

func normalizeRemotePlatform(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "linux":
		return "linux"
	case "darwin":
		return "darwin"
	default:
		return ""
	}
}

func normalizeRemoteArch(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "x86_64", "amd64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return ""
	}
}
