package webui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// CheckPortAvailable checks if a port is available to bind to
func CheckPortAvailable(port int) bool {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false // Port is in use
	}
	listener.Close()
	return true // Port is free
}

// FindAvailablePort finds an available port starting from a base port
func FindAvailablePort(basePort int) (int, error) {
	port := basePort
	for port < basePort+100 {
		if CheckPortAvailable(port) {
			return port, nil
		}
		port++
	}
	return 0, fmt.Errorf("no available port found in range %d-%d", basePort, basePort+99)
}

// formatListenAddr constructs a listen address string in "host:port" format,
// using bracket notation for IPv6 addresses (e.g., "[::]:56000").
func formatListenAddr(host string, port int) string {
	if strings.Contains(host, ":") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// DisplayAddr returns a user-friendly address string for display in logs.
func DisplayAddr(bindAddr string) string {
	switch bindAddr {
	case "127.0.0.1", "0.0.0.0", "::", "::1":
		return "localhost"
	}
	return bindAddr
}

// normalizeOriginForCompare normalizes a parsed URL for case-insensitive
// origin comparison. It lowercases the scheme and host, and strips default
// ports (80 for HTTP, 443 for HTTPS) so that e.g. "https://example.com"
// and "https://example.com:443" are treated as equivalent.
func normalizeOriginForCompare(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		return scheme + "://" + host + ":" + port
	}
	return scheme + "://" + host
}

// expandHomeVar expands only $HOME and ${HOME} references in a path string.
// This is more restrictive than os.ExpandEnv (which expands all env vars)
// and avoids surprising behavior from arbitrary environment variable expansion.
func expandHomeVar(path string) string {
	home := os.Getenv("HOME")
	if home == "" {
		return path
	}
	path = strings.ReplaceAll(path, "${HOME}", home)
	path = strings.ReplaceAll(path, "$HOME", home)
	return path
}

func filepathAbsEval(path string) (string, error) {
	// Expand $HOME / ${HOME} and tilde in the path.
	expanded := expandHomeVar(path)
	if strings.HasPrefix(expanded, "~/") || expanded == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if expanded == "~" {
			expanded = home
		} else {
			expanded = filepath.Join(home, expanded[2:])
		}
	}

	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if os.IsNotExist(err) {
			// Fallback to unresolved absolute path. This is safe because callers
			// (e.g., SetWorkspaceRoot via isWithinWorkspace) validate the path
			// is within the workspace before it's used.
			return abs, nil
		}
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	return resolved, nil
}

func isExpectedServerCloseError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return true
	}
	// Go stdlib may wrap this in plain text depending on call path.
	return strings.Contains(strings.ToLower(err.Error()), "use of closed network connection")
}
