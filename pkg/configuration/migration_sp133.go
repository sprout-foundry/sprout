package configuration

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// migrationMarker is the sentinel file written into the state dir
// after SP-133 migration completes. Its presence prevents re-running
// the migration on every startup.
const migrationMarker = "migrated_sp133"

// legacyDir is the pre-SP-133 root (~/.sprout) that held config, state,
// cache, and secrets in a single directory.
const legacyDir = ".sprout"

// categories maps each legacy subdirectory/file to its new category root.
// The key is the legacy path relative to ~/.sprout; the value is the
// new resolver function.
//
// Files that map to "config" stay where they are (config dir is already
// ~/.config/sprout — the legacy config.json is read by the legacy
// fallback in config_paths.go, not moved). Files that map to "state",
// "cache", or "data" are relocated.
var stateFiles = []string{
	"recent_workspaces.json",
	"workspace_consent.json",
	"instances.json",
	"state.json",
	"webui_host.json",
	"ssh_sessions.json",
	"service.env",
	"shell-audit.jsonl",
	"cost_history.json",
	"daemon.pid",
	"workspace.log",
}

var stateDirs = []string{
	"sessions",
	"transcripts",
	"runlogs",
	"shell_outputs",
	"logs",
}

var cacheFiles = []string{
	"lastRequest.json",
	"lastResponse.json",
}

var cacheDirs = []string{
	"search_cache",
	"url_cache",
}

// credentialsFiles are the sensitive files that move to config/credentials/.
var credentialsFiles = []string{
	"api_keys.json",
	"key.age",
	"keyring_providers.json",
	"api_keys.mode",
	"backend.mode",
}

// NeedsMigration returns true when a legacy ~/.sprout directory exists
// and no migration marker is present in the state dir.
func NeedsMigration() bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	legacy := filepath.Join(home, legacyDir)
	if _, err := os.Stat(legacy); os.IsNotExist(err) {
		return false
	}
	// Check for marker
	stateDir, err := envutil.StateDir()
	if err == nil {
		marker := filepath.Join(stateDir, migrationMarker)
		if _, err := os.Stat(marker); err == nil {
			return false
		}
	}
	return true
}

// RunMigration moves each category of data from the legacy ~/.sprout
// directory to its new root. It is idempotent: already-moved files are
// skipped, and a marker prevents re-entry.
//
// The legacy directory is left in place (empty of moved content) so a
// failed migration is diagnosable.
func RunMigration() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}
	legacy := filepath.Join(home, legacyDir)
	if _, err := os.Stat(legacy); os.IsNotExist(err) {
		return nil
	}

	configRoot, err := envutil.ConfigDir()
	if err != nil {
		return fmt.Errorf("resolve config dir: %w", err)
	}
	stateRoot, err := envutil.StateDir()
	if err != nil {
		return fmt.Errorf("resolve state dir: %w", err)
	}
	cacheRoot, err := envutil.CacheDir()
	if err != nil {
		return fmt.Errorf("resolve cache dir: %w", err)
	}

	var moved int
	var skipped int

	// 1. Move state files
	for _, name := range stateFiles {
		src := filepath.Join(legacy, name)
		dst := filepath.Join(stateRoot, name)
		m, s, err := migrateFile(src, dst)
		if err != nil {
			log.Printf("[migration] warning: %s: %v", name, err)
			continue
		}
		moved += m
		skipped += s
	}

	// 2. Move state directories
	for _, name := range stateDirs {
		src := filepath.Join(legacy, name)
		dst := filepath.Join(stateRoot, name)
		m, s, err := migrateDir(src, dst)
		if err != nil {
			log.Printf("[migration] warning: %s/: %v", name, err)
			continue
		}
		moved += m
		skipped += s
	}

	// 3. Move cache files
	for _, name := range cacheFiles {
		src := filepath.Join(legacy, name)
		dst := filepath.Join(cacheRoot, "diagnostics", name)
		m, s, err := migrateFile(src, dst)
		if err != nil {
			log.Printf("[migration] warning: %s: %v", name, err)
			continue
		}
		moved += m
		skipped += s
	}

	// 4. Move cache directories
	for _, name := range cacheDirs {
		src := filepath.Join(legacy, name)
		dst := filepath.Join(cacheRoot, name)
		m, s, err := migrateDir(src, dst)
		if err != nil {
			log.Printf("[migration] warning: %s/: %v", name, err)
			continue
		}
		moved += m
		skipped += s
	}

	// 5. Move credentials (copy-verify-then-remove)
	// SP-133 audit item A: verify a successful decrypt of at least one key
	// before removing the source. A bug that corrupts api_keys.json or
	// key.age during the copy is unrecoverable without re-entering every key.
	credDir := filepath.Join(configRoot, CredentialsDirName)
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		log.Printf("[migration] warning: create credentials dir: %v", err)
	} else {
		// First copy all credential files without removing sources
		var anyCredMoved bool
		for _, name := range credentialsFiles {
			src := filepath.Join(legacy, name)
			dst := filepath.Join(credDir, name)
			if _, err := os.Stat(src); os.IsNotExist(err) {
				continue
			}
			if _, err := os.Stat(dst); err != nil {
				if err := copyFile(src, dst); err != nil {
					log.Printf("[migration] warning: copy %s: %v", name, err)
					continue
				}
				if !verifyCopy(src, dst) {
					log.Printf("[migration] warning: verify failed for %s, keeping source", name)
					os.Remove(dst)
					continue
				}
				anyCredMoved = true
			} else {
				// Dst already exists — source will be removed below
				anyCredMoved = true
			}
		}

		// Verify decryption before removing sources. Only remove the
		// legacy copies if the new location decrypts successfully OR
		// there are no keys to decrypt (empty store).
		if anyCredMoved {
			canRemove := true
			if store, err := credentials.LoadFromDir(credDir); err != nil {
				log.Printf("[migration] warning: credential verification failed at new location, keeping source files: %v", err)
				canRemove = false
			} else if len(store) > 0 {
				log.Printf("[migration] verified %d credential(s) decrypt successfully at new location", len(store))
			}

			if canRemove {
				for _, name := range credentialsFiles {
					os.Remove(filepath.Join(legacy, name))
				}
			}
		}
	}

	// 6. Write marker
	marker := filepath.Join(stateRoot, migrationMarker)
	if err := os.WriteFile(marker, []byte("1\n"), 0600); err != nil {
		log.Printf("[migration] warning: write marker: %v", err)
	}

	log.Printf("[migration] SP-133 complete: %d files moved, %d skipped. Legacy dir left at %s", moved, skipped, legacy)
	return nil
}

// migrateFile copies src to dst if src exists and dst doesn't, then
// removes src. If dst already exists, src is removed without copying.
// Returns (moved=1, skipped=0) on success, (0, 1) when src doesn't exist.
func migrateFile(src, dst string) (moved, skipped int, err error) {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return 0, 1, nil
	}
	// Ensure dst dir exists
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return 0, 0, fmt.Errorf("create dst dir: %w", err)
	}
	if _, err := os.Stat(dst); err == nil {
		// Dst already exists — just remove the legacy copy
		os.Remove(src)
		return 0, 1, nil
	}
	if err := copyFile(src, dst); err != nil {
		return 0, 0, err
	}
	// Verify the copy
	if !verifyCopy(src, dst) {
		return 0, 0, fmt.Errorf("verify failed: %s → %s", src, dst)
	}
	os.Remove(src)
	return 1, 0, nil
}

// migrateDir moves a directory from src to dst. If dst exists, files
// are merged.
func migrateDir(src, dst string) (moved, skipped int, err error) {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return 0, 1, nil
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return 0, 0, fmt.Errorf("create dst dir: %w", err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return 0, 0, fmt.Errorf("read src dir: %w", err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			m, s, e := migrateDir(srcPath, dstPath)
			moved += m
			skipped += s
			if e != nil {
				err = e
			}
		} else {
			m, s, e := migrateFile(srcPath, dstPath)
			moved += m
			skipped += s
			if e != nil {
				err = e
			}
		}
	}
	// Remove the now-empty source dir
	os.Remove(src)
	return moved, skipped, err
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("stat src: %w", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	return nil
}

func verifyCopy(src, dst string) bool {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return false
	}
	return srcInfo.Size() == dstInfo.Size()
}
