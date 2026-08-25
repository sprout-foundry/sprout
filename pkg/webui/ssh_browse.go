//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"strings"
)

func browseSSHDirectory(hostAlias, requestedPath string) ([]sshRemoteEntry, string, string, error) {
	hostAlias = strings.TrimSpace(hostAlias)
	if hostAlias == "" {
		return nil, "", "", fmt.Errorf("SSH host alias is required")
	}
	if err := ensureSSHProgramsAvailable(); err != nil {
		return nil, "", "", fmt.Errorf("ensure SSH programs available: %w", err)
	}

	targetPath := strings.TrimSpace(requestedPath)
	if targetPath == "" {
		targetPath = "$HOME"
	}

	pythonSnippet := strings.Join([]string{
		"import json, os, sys",
		"target = os.path.abspath(os.path.expanduser(sys.argv[1]))",
		"home = os.path.abspath(os.path.expanduser('~'))",
		"if not os.path.isdir(target):",
		"    print(f'directory not found: {target}', file=sys.stderr)",
		"    raise SystemExit(1)",
		"entries = []",
		"for name in sorted(os.listdir(target), key=str.lower):",
		"    if name.startswith('.'):",
		"        continue",
		"    path = os.path.join(target, name)",
		"    if os.path.isdir(path):",
		"        entries.append({'name': name, 'path': path, 'type': 'directory'})",
		"# Sentinel marker so login-profile stdout (MOTD, fortune) doesn't",
		"# corrupt the JSON parsing on the caller side.",
		"print('SPROUT_DIR_LISTING_START')",
		"print(json.dumps({'path': target, 'home_path': home, 'files': entries}))",
	}, "\n")

	script := strings.Join([]string{
		"set -e",
		fmt.Sprintf("TARGET_INPUT=%s", shellEscapeSSH(targetPath)),
		`if [ "$TARGET_INPUT" = '$HOME' ]; then`,
		`  TARGET_INPUT="$HOME"`,
		"fi",
		`if command -v python3 >/dev/null 2>&1; then`,
		fmt.Sprintf("  python3 - \"$TARGET_INPUT\" <<'PY'\n%s\nPY", pythonSnippet),
		`elif command -v python >/dev/null 2>&1; then`,
		fmt.Sprintf("  python - \"$TARGET_INPUT\" <<'PY'\n%s\nPY", pythonSnippet),
		"else",
		`  echo "python3 or python is required on the remote host" >&2`,
		"  exit 1",
		"fi",
	}, "\n")

	cmd := newSSHCommand(hostAlias, script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		details := trimSSHOutput(out)
		if details == "" {
			details = err.Error()
		}
		return nil, "", "", fmt.Errorf("SSH command failed: %s: %w", details, err)
	}

	// Extract JSON after the sentinel marker — login-profile stdout
	// (MOTD, fortune) can prepend output that would break json.Unmarshal.
	jsonOutput, err := extractSentinelResult(string(out), "SPROUT_DIR_LISTING_START")
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to find directory listing marker: %w", err)
	}

	var payload struct {
		Path     string           `json:"path"`
		HomePath string           `json:"home_path"`
		Files    []sshRemoteEntry `json:"files"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(jsonOutput)), &payload); err != nil {
		return nil, "", "", fmt.Errorf("failed to decode ssh directory listing: %w", err)
	}

	return payload.Files, strings.TrimSpace(payload.Path), strings.TrimSpace(payload.HomePath), nil
}
