//go:build !js

package skills

// install_sources.go — the skills URL / registry install sources: the
// InstallFromURL and InstallFromRegistry entry points and the tar
// extraction helpers they use (untarFileReader, untarReader). Split out of
// install.go (both files carry the //go:build !js tag).

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// InstallFromURL fetches a URL and installs the skill(s) found.
func InstallFromURL(ctx context.Context, url string, opts InstallOptions) ([]InstallResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch URL: status %d", resp.StatusCode)
	}

	tmpDir, err := os.MkdirTemp("", "sprout-skill-url-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Check if it's a tarball
	isTarball := strings.HasSuffix(url, ".tar.gz") || strings.HasSuffix(url, ".tgz")
	contentType := resp.Header.Get("Content-Type")
	if !isTarball && (strings.Contains(contentType, "gzip") || strings.Contains(contentType, "tar")) {
		isTarball = true
	}

	var skillPaths []string
	if isTarball {
		if err := untarFileReader(resp.Body, tmpDir); err != nil {
			return nil, fmt.Errorf("untar: %w", err)
		}
		skillPaths, err = findSkillMD(tmpDir)
		if err != nil {
			return nil, err
		}
	} else {
		// Treat as a single SKILL.md file
		skillFile := filepath.Join(tmpDir, SkillFileName)
		w, err := os.Create(skillFile)
		if err != nil {
			return nil, fmt.Errorf("create temp SKILL.md: %w", err)
		}
		if _, err := io.Copy(w, resp.Body); err != nil {
			w.Close()
			return nil, fmt.Errorf("write SKILL.md: %w", err)
		}
		w.Close()
		skillPaths = []string{skillFile}
	}

	var results []InstallResult
	for _, skillMD := range skillPaths {
		srcDir := filepath.Dir(skillMD)
		content, err := os.ReadFile(skillMD)
		if err != nil {
			return results, fmt.Errorf("read SKILL.md: %w", err)
		}
		fm, err := parseSkillFrontmatter(string(content))
		if err != nil {
			return results, fmt.Errorf("parse SKILL.md: %w", err)
		}

		skillID := fm.Name

		origin := Origin{
			Type:        "url",
			URL:         url,
			InstalledAt: time.Now(),
		}

		result, err := installSkill(srcDir, skillID, origin, opts)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}

	return results, nil
}

// InstallFromRegistry installs a skill by registry ID from the embedded registry.
// It looks up the entry, clones the git repo (or uses a local path in test mode),
// extracts the skill subdirectory, and installs the found SKILL.md.
func InstallFromRegistry(ctx context.Context, registryID string, opts InstallOptions) ([]InstallResult, error) {
	registry, isTestOverride, err := effectiveRegistry()
	if err != nil {
		return nil, fmt.Errorf("install from registry: %w", err)
	}

	entry, err := registry.LookupByID(registryID)
	if err != nil {
		return nil, fmt.Errorf("install from registry: %w", err)
	}

	// Check if we're in test override mode with a local file:// URL.
	// This allows tests to avoid network/git dependencies.
	localPath := ""
	if isTestOverride && strings.HasPrefix(entry.GitURL, "file://") {
		localPath = strings.TrimPrefix(entry.GitURL, "file://")
	}

	tmpDir, err := os.MkdirTemp("", "sprout-skill-registry-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	// NOTE: tmpDir is reassigned below (to stageDir) in the production path,
	// but the defer above already captured the original clone dir value at
	// defer-time, so both dirs get cleaned up independently.

	if localPath != "" {
		// Test override: copy from local path instead of git clone.
		srcSubdir := filepath.Join(localPath, entry.PathInRepo)

		// Validate that the resolved path stays inside the local source root.
		cleanBase := filepath.Clean(localPath)
		cleanSub := filepath.Clean(srcSubdir)
		rel, relErr := filepath.Rel(cleanBase, cleanSub)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("path_in_repo escapes clone root: %s", entry.PathInRepo)
		}

		if _, err := os.Stat(srcSubdir); os.IsNotExist(err) {
			return nil, fmt.Errorf("registry skill path not found: %s", srcSubdir)
		}
		if err := copyDir(srcSubdir, tmpDir); err != nil {
			return nil, fmt.Errorf("copy skill dir: %w", err)
		}
	} else {
		// Production: clone from git.
		if !gitAvailable() {
			return nil, ErrGitNotAvailable
		}

		cloneArgs := []string{"clone", "--depth", "1", "--branch", entry.GitRef, entry.GitURL, tmpDir}
		cmd := exec.CommandContext(ctx, "git", cloneArgs...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git clone: %s: %w", string(out), err)
		}

		// Locate the skill subdirectory within the cloned repo.
		srcSubdir := filepath.Join(tmpDir, entry.PathInRepo)

		// Validate that path_in_repo stays inside the clone root.
		cleanBase := filepath.Clean(tmpDir)
		cleanSub := filepath.Clean(srcSubdir)
		rel, relErr := filepath.Rel(cleanBase, cleanSub)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("path_in_repo escapes clone root: %s", entry.PathInRepo)
		}

		if _, err := os.Stat(srcSubdir); os.IsNotExist(err) {
			return nil, fmt.Errorf("path_in_repo not found in cloned repo: %s", srcSubdir)
		}

		// Copy the subdirectory to a fresh staging dir.
		stageDir, err := os.MkdirTemp("", "sprout-skill-stage-*")
		if err != nil {
			return nil, fmt.Errorf("create staging dir: %w", err)
		}
		defer os.RemoveAll(stageDir)

		if err := copyDir(srcSubdir, stageDir); err != nil {
			return nil, fmt.Errorf("copy skill subdirectory: %w", err)
		}

		// Swap tmpDir to point at the staged content.
		// The original clone dir is still cleaned up by the earlier defer.
		tmpDir = stageDir
	}

	// Find SKILL.md in the staged directory.
	skills, err := findSkillMD(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("find SKILL.md: %w", err)
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("no SKILL.md files found in registry skill directory")
	}

	// Install the first (and expected single) SKILL.md.
	skillMD := skills[0]
	srcDir := filepath.Dir(skillMD)
	content, err := os.ReadFile(skillMD)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}
	fm, err := parseSkillFrontmatter(string(content))
	if err != nil {
		return nil, err
	}

	skillID := fm.Name

	origin := Origin{
		Type:        "registry",
		URL:         entry.GitURL,
		Ref:         entry.GitRef,
		RegistryID:  entry.ID,
		InstalledAt: time.Now(),
	}

	result, err := installSkill(srcDir, skillID, origin, opts)
	if err != nil {
		return nil, err
	}
	return []InstallResult{result}, nil
}

func untarFileReader(r io.Reader, dst string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		// Maybe it's a plain tar
		tr := tar.NewReader(r)
		return untarReader(tr, dst)
	}
	defer gr.Close()

	return untarReader(tar.NewReader(gr), dst)
}

func untarReader(tr *tar.Reader, dst string) error {
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Reject symlinks/hardlinks to prevent malicious tarballs from
		// pointing files outside the destination.
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			return fmt.Errorf("symlinks/hardlinks not allowed in skill tarball: %s", header.Name)
		}

		target := filepath.Join(dst, header.Name)

		// Guard against path-traversal: every entry must resolve inside dst.
		cleanDst := filepath.Clean(dst)
		cleanTarget := filepath.Clean(target)
		rel, relErr := filepath.Rel(cleanDst, cleanTarget)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("tar entry escapes destination: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}

		case tar.TypeReg:
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}
