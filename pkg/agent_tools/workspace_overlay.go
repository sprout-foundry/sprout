// Workspace overlay loader for SP-049-2c.
//
// Reads .sprout/shell-policy.json from the workspace root and merges it into
// the effective shell policy according to the user's workspace_overlay.mode
// setting. Manages trust hashes in ~/.sprout/trusted-workspaces.json so that
// workspace-provided safe patterns are only honored after explicit trust.
package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

const (
	// workspaceOverlayFileName is the name of the workspace policy file inside
	// the .sprout directory at the workspace root.
	workspaceOverlayFileName = "shell-policy.json"

	// trustStoreFileName is the name of the trust store file in the user's
	// ~/.sprout/ directory.
	trustStoreFileName = "trusted-workspaces.json"
)

// trustedWorkspacesPath holds the absolute path to the trust store file.
var trustedWorkspacesPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback: should never happen on a real system.
		return ".sprout/trusted-workspaces.json"
	}
	return filepath.Join(home, ".sprout", trustStoreFileName)
}()

// trustedWorkspacesMu protects the in-memory trusted workspaces cache.
var trustedWorkspacesMu sync.RWMutex

// trustedWorkspacesCache is the in-memory cache of workspace trust hashes.
// Maps absolute workspace path → SHA-256 hex digest of its shell-policy.json.
// Populated lazily on first access.
var trustedWorkspacesCache map[string]string

// loadTrustedWorkspaces reads the trust store JSON file into the in-memory
// cache. Callers should hold trustedWorkspacesMu for writing if they intend
// to modify the cache afterward. Idempotent: if the cache is already
// populated it returns immediately without re-reading from disk — callers
// that need a fresh reload must nil the cache first.
func loadTrustedWorkspaces() error {
	if trustedWorkspacesCache != nil {
		return nil // Already loaded.
	}

	data, err := os.ReadFile(trustedWorkspacesPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet — start with an empty map.
			trustedWorkspacesCache = make(map[string]string)
			return nil
		}
		return fmt.Errorf("read trust store: %w", err)
	}

	var store map[string]string
	if err := json.Unmarshal(data, &store); err != nil {
		return fmt.Errorf("parse trust store: %w", err)
	}
	trustedWorkspacesCache = store
	return nil
}

// saveTrustedWorkspaces writes the in-memory trusted workspaces cache to disk.
// Creates the parent directory (0o700 — security-sensitive) if it doesn't exist.
func saveTrustedWorkspaces() error {
	if trustedWorkspacesCache == nil {
		return nil // Nothing to save.
	}

	dir := filepath.Dir(trustedWorkspacesPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create trust store directory: %w", err)
	}

	data, err := json.MarshalIndent(trustedWorkspacesCache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trust store: %w", err)
	}

	return os.WriteFile(trustedWorkspacesPath, data, 0o600)
}

// workspacePolicyPath returns the absolute path to the workspace's
// .sprout/shell-policy.json file.
func workspacePolicyPath(workspaceRoot string) (string, error) {
	abs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	return filepath.Join(abs, ".sprout", workspaceOverlayFileName), nil
}

// readWorkspacePolicy reads and unmarshals the workspace policy file into a
// ShellConfig. Returns the config and the SHA-256 hex digest of the raw file
// bytes (for trust verification).
func readWorkspacePolicy(workspaceRoot string) (configuration.ShellConfig, string, error) {
	path, err := workspacePolicyPath(workspaceRoot)
	if err != nil {
		return configuration.ShellConfig{}, "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return configuration.ShellConfig{}, "", fmt.Errorf("workspace policy file not found: %s: %w", path, os.ErrNotExist)
		}
		return configuration.ShellConfig{}, "", fmt.Errorf("read workspace policy: %w", err)
	}

	var cfg configuration.ShellConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return configuration.ShellConfig{}, "", fmt.Errorf("parse workspace policy: %w", err)
	}

	hash := sha256Hex(data)
	return cfg, hash, nil
}

// sha256Hex returns the SHA-256 hex digest of the given bytes.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// deepCopyShellConfig returns a deep copy of the given ShellConfig. Slice
// fields are duplicated so the caller can mutate the result without affecting
// the original.
//
// Safety note: ShellPattern is a pure-value struct (Match/Kind/Reason are all
// strings), so copy() on the slice produces a true deep copy. If pointer or
// slice fields are ever added to ShellPattern, this function must be updated
// to copy them recursively.
func deepCopyShellConfig(src configuration.ShellConfig) configuration.ShellConfig {
	dst := src
	if src.UserSafePatterns != nil {
		dst.UserSafePatterns = make([]configuration.ShellPattern, len(src.UserSafePatterns))
		copy(dst.UserSafePatterns, src.UserSafePatterns)
	}
	if src.UserDangerousPatterns != nil {
		dst.UserDangerousPatterns = make([]configuration.ShellPattern, len(src.UserDangerousPatterns))
		copy(dst.UserDangerousPatterns, src.UserDangerousPatterns)
	}
	return dst
}

// LoadWorkspaceOverlay reads the workspace policy file (.sprout/shell-policy.json)
// from the given workspace root and merges it into userCfg according to the
// configured workspace_overlay.mode.
//
// Mode resolution:
//
//   - "ignore": Returns userCfg unchanged with no warnings.
//   - "tighten_only" (default): Appends the workspace's user_dangerous_patterns
//     to userCfg. The workspace's user_safe_patterns are ignored with a warning
//     to prevent supply-chain attacks (a cloned repo silently silencing prompts).
//   - "trusted": If the workspace's current policy file hash matches the
//     recorded hash in ~/.sprout/trusted-workspaces.json, the full overlay
//     (both safe and dangerous patterns) is appended to userCfg. If the hash
//     does not match (file changed since trust was recorded) or no hash is
//     recorded, falls back to "tighten_only" behavior with a warning.
//
// Returns the merged ShellConfig (a new value — userCfg is not mutated) and
// a slice of warning messages (may be empty).
func LoadWorkspaceOverlay(workspaceRoot string, userCfg configuration.ShellConfig) (configuration.ShellConfig, []string) {
	var warnings []string

	// Normalize mode.
	mode := userCfg.WorkspaceOverlay.Mode
	if mode == "" {
		mode = "tighten_only"
	}

	// "ignore" — short-circuit.
	if mode == "ignore" {
		return userCfg, warnings
	}

	// Read workspace policy file.
	wsCfg, _, err := readWorkspacePolicy(workspaceRoot)
	if err != nil {
		// Emit a warning for errors that are NOT "file not found" (e.g.
		// permission denied, invalid JSON) so the user is aware something
		// is wrong with their workspace policy file. Missing files are
		// common (overlay not configured) and silently ignored.
		if !errors.Is(err, os.ErrNotExist) {
			warnings = append(warnings, fmt.Sprintf("workspace policy error for %s: %v; using user config only", workspaceRoot, err))
		}
		return userCfg, warnings
	}

	// Start with a deep copy so we never mutate the caller's config.
	merged := deepCopyShellConfig(userCfg)

	switch mode {
	case "tighten_only":
		applyTightenOnly(&merged, wsCfg, workspaceRoot, &warnings)
	case "trusted":
		applyTrustedMode(&merged, wsCfg, workspaceRoot, &warnings)
	default:
		// Unrecognized mode — fall back to tighten_only behavior.
		warnings = append(warnings, fmt.Sprintf("workspace overlay: unrecognized mode %q, falling back to tighten_only", mode))
		applyTightenOnly(&merged, wsCfg, workspaceRoot, &warnings)
	}

	return merged, warnings
}

// applyTightenOnly appends only dangerous patterns from the workspace config,
// ignoring safe patterns with a warning.
func applyTightenOnly(merged *configuration.ShellConfig, wsCfg configuration.ShellConfig, workspaceRoot string, warnings *[]string) {
	if len(wsCfg.UserDangerousPatterns) > 0 {
		merged.UserDangerousPatterns = append(merged.UserDangerousPatterns, wsCfg.UserDangerousPatterns...)
	}
	if len(wsCfg.UserSafePatterns) > 0 {
		*warnings = append(*warnings, fmt.Sprintf("workspace overlay (tighten_only): ignoring %d user_safe_patterns from %s",
			len(wsCfg.UserSafePatterns), workspaceRoot))
	}
}

// applyTrustedMode checks the trust store and either applies the full overlay
// (hash matches) or falls back to tighten_only behavior (hash mismatch).
func applyTrustedMode(merged *configuration.ShellConfig, wsCfg configuration.ShellConfig, workspaceRoot string, warnings *[]string) {
	absPath, err := filepath.Abs(workspaceRoot)
	if err != nil {
		absPath = workspaceRoot
	}

	// Read the workspace policy again to get the hash for trust verification.
	_, wsHash, err := readWorkspacePolicy(absPath)
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("workspace trust check failed for %s: %v; falling back to tighten_only",
			workspaceRoot, err))
		applyTightenOnly(merged, wsCfg, workspaceRoot, warnings)
		return
	}

	trusted, err := isWorkspaceTrustedRaw(absPath, wsHash)
	if err != nil {
		// Can't verify trust — fall back to tighten_only.
		*warnings = append(*warnings, fmt.Sprintf("workspace trust check failed for %s: %v; falling back to tighten_only",
			workspaceRoot, err))
		applyTightenOnly(merged, wsCfg, workspaceRoot, warnings)
		return
	}

	if trusted {
		// Full overlay — both safe and dangerous.
		if len(wsCfg.UserSafePatterns) > 0 {
			merged.UserSafePatterns = append(merged.UserSafePatterns, wsCfg.UserSafePatterns...)
		}
		if len(wsCfg.UserDangerousPatterns) > 0 {
			merged.UserDangerousPatterns = append(merged.UserDangerousPatterns, wsCfg.UserDangerousPatterns...)
		}
		return
	}

	// Hash mismatch or not recorded — fall back to tighten_only.
	*warnings = append(*warnings, fmt.Sprintf("workspace %s is not trusted (hash mismatch or not recorded); falling back to tighten_only",
		workspaceRoot))
	applyTightenOnly(merged, wsCfg, workspaceRoot, warnings)
}

// isWorkspaceTrustedRaw checks if the given absolute path has a matching hash
// in the trust store. The caller is responsible for path normalization.
func isWorkspaceTrustedRaw(absPath string, hash string) (bool, error) {
	trustedWorkspacesMu.RLock()
	defer trustedWorkspacesMu.RUnlock()

	if err := loadTrustedWorkspaces(); err != nil {
		return false, err
	}
	if trustedWorkspacesCache == nil {
		return false, nil
	}
	recorded, ok := trustedWorkspacesCache[absPath]
	if !ok {
		return false, nil
	}
	return recorded == hash, nil
}

// TrustWorkspace computes the SHA-256 hash of the workspace's
// .sprout/shell-policy.json and records it in ~/.sprout/trusted-workspaces.json.
// Returns an error if the workspace policy file doesn't exist or can't be read.
func TrustWorkspace(workspaceRoot string) error {
	absPath, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}

	_, hash, err := readWorkspacePolicy(absPath)
	if err != nil {
		return err
	}

	trustedWorkspacesMu.Lock()
	defer trustedWorkspacesMu.Unlock()

	// Force a fresh reload from disk — another goroutine or process may have
	// modified the trust store since our last read.
	trustedWorkspacesCache = nil
	if err := loadTrustedWorkspaces(); err != nil {
		return err
	}
	trustedWorkspacesCache[absPath] = hash

	return saveTrustedWorkspaces()
}

// UntrustWorkspace removes the workspace from the trust store.
func UntrustWorkspace(workspaceRoot string) error {
	absPath, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}

	trustedWorkspacesMu.Lock()
	defer trustedWorkspacesMu.Unlock()

	// Force a fresh reload from disk.
	trustedWorkspacesCache = nil
	if err := loadTrustedWorkspaces(); err != nil {
		return err
	}
	delete(trustedWorkspacesCache, absPath)

	return saveTrustedWorkspaces()
}

// UntrustAllWorkspaces clears the entire trust store, removing all recorded
// workspace hashes.
func UntrustAllWorkspaces() error {
	trustedWorkspacesMu.Lock()
	defer trustedWorkspacesMu.Unlock()

	// Force a fresh reload from disk, then clear.
	trustedWorkspacesCache = nil
	if err := loadTrustedWorkspaces(); err != nil {
		return err
	}
	trustedWorkspacesCache = make(map[string]string)

	return saveTrustedWorkspaces()
}

// IsWorkspaceTrusted checks if the workspace's current .sprout/shell-policy.json
// hash matches the recorded hash in the trust store. Returns false (not an
// error) if the workspace has never been trusted or if the file doesn't exist.
func IsWorkspaceTrusted(workspaceRoot string) (bool, error) {
	absPath, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false, fmt.Errorf("resolve workspace root: %w", err)
	}

	_, hash, err := readWorkspacePolicy(absPath)
	if err != nil {
		// File doesn't exist — not trusted.
		return false, nil
	}

	return isWorkspaceTrustedRaw(absPath, hash)
}

// LoadTrustedWorkspaces reloads the trust store from disk into the in-memory
// cache, forcing a refresh regardless of cache state.
func LoadTrustedWorkspaces() error {
	trustedWorkspacesMu.Lock()
	defer trustedWorkspacesMu.Unlock()

	// Force reload — nil the cache so loadTrustedWorkspaces re-reads from disk.
	trustedWorkspacesCache = nil
	return loadTrustedWorkspaces()
}

// SaveTrustedWorkspaces writes the in-memory trust store cache to disk.
func SaveTrustedWorkspaces() error {
	trustedWorkspacesMu.RLock()
	defer trustedWorkspacesMu.RUnlock()
	return saveTrustedWorkspaces()
}
