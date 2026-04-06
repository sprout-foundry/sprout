package webui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultConsentTTL = 2 * time.Minute
)

type fileConsentGrant struct {
	CanonicalPath string
	Operation     string
	ExpiresAt     time.Time
}

type fileConsentManager struct {
	mutex  sync.Mutex
	grants map[string]fileConsentGrant
}

func newFileConsentManager() *fileConsentManager {
	return &fileConsentManager{grants: make(map[string]fileConsentGrant)}
}

func (m *fileConsentManager) issue(canonicalPath, operation string, ttl time.Duration) (string, time.Time, error) {
	if ttl <= 0 {
		ttl = defaultConsentTTL
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate consent token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(ttl)

	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.cleanupExpiredLocked()
	m.grants[token] = fileConsentGrant{
		CanonicalPath: canonicalPath,
		Operation:     operation,
		ExpiresAt:     expiresAt,
	}

	return token, expiresAt, nil
}

func (m *fileConsentManager) consume(token, canonicalPath, operation string) bool {
	if token == "" {
		return false
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.cleanupExpiredLocked()

	grant, ok := m.grants[token]
	if !ok {
		return false
	}
	if grant.CanonicalPath != canonicalPath || grant.Operation != operation {
		return false
	}

	delete(m.grants, token)
	return true
}

func (m *fileConsentManager) cleanupExpiredLocked() {
	now := time.Now()
	for token, grant := range m.grants {
		if now.After(grant.ExpiresAt) {
			delete(m.grants, token)
		}
	}
}

func (m *fileConsentManager) clearAll() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.grants = make(map[string]fileConsentGrant)
}

func canonicalizePath(path string, workspaceRoot string, forWrite bool) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("file path is required")
	}

	cleaned := filepath.Clean(trimmed)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(workspaceRoot, cleaned)
	}

	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	if !forWrite {
		resolved, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("path does not exist: %s", absPath)
			}
			return "", fmt.Errorf("failed to resolve path: %w", err)
		}
		return resolved, nil
	}

	// For writes, the file might not exist yet. Resolve symlinks on the nearest existing parent.
	relativeSuffix := ""
	probe := absPath
	for {
		info, err := os.Stat(probe)
		if err == nil {
			if !info.IsDir() && probe == absPath {
				// Existing file path.
				resolved, err := filepath.EvalSymlinks(absPath)
				if err != nil {
					return "", fmt.Errorf("failed to resolve path: %w", err)
				}
				return resolved, nil
			}
			resolvedParent, err := filepath.EvalSymlinks(probe)
			if err != nil {
				return "", fmt.Errorf("failed to resolve path: %w", err)
			}
			if relativeSuffix == "" {
				return resolvedParent, nil
			}
			return filepath.Join(resolvedParent, relativeSuffix), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to inspect path: %w", err)
		}

		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}

		base := filepath.Base(probe)
		if relativeSuffix == "" {
			relativeSuffix = base
		} else {
			relativeSuffix = filepath.Join(base, relativeSuffix)
		}
		probe = parent
	}

	return absPath, nil
}

func isWithinWorkspace(path, workspaceRoot string) bool {
	rel, err := filepath.Rel(workspaceRoot, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

// isAppConfigPath returns true if the path is inside the ledit configuration
// directory (e.g. ~/.ledit/ or ~/.config/ledit/). These are workspace-owned
// config files and should never trigger the external-path consent prompt.
func isAppConfigPath(path string) bool {
	configDir, err := getConfigDir()
	if err != nil || configDir == "" {
		return false
	}
	absConfDir, err := filepath.Abs(configDir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absConfDir, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

// getConfigDir returns the ledit configuration directory, memoized across calls.
// It uses the same logic as configuration.GetConfigDir but is kept in the webui
// package to avoid pulling in the full configuration import graph.
var configDirCache string
var configDirErr error
var configDirOnce sync.Once

func getConfigDir() (string, error) {
	configDirOnce.Do(func() {
		configDirCache, configDirErr = resolveConfigDir()
	})
	if configDirErr != nil {
		return "", fmt.Errorf("failed to get config directory: %w", configDirErr)
	}
	return configDirCache, nil
}

func resolveConfigDir() (string, error) {
	if d := strings.TrimSpace(os.Getenv("LEDIT_CONFIG")); d != "" {
		abs, err := filepath.Abs(d)
		if err != nil {
			return "", fmt.Errorf("resolve LEDIT_CONFIG path: %w", err)
		}
		return abs, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get user home directory: %w", err)
	}

	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome != "" {
		abs, err := filepath.Abs(configHome)
		if err != nil {
			return "", fmt.Errorf("resolve XDG_CONFIG_HOME path: %w", err)
		}
		return filepath.Join(abs, "ledit"), nil
	}

	return filepath.Join(homeDir, ".ledit"), nil
}
