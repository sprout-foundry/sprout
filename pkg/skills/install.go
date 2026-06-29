//go:build !js

package skills

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const OriginMetadataFile = ".sprout-origin.json"

var (
	ErrInvalidFrontmatter = errors.New("invalid skill frontmatter")
	ErrAlreadyInstalled   = errors.New("skill already installed")
	ErrNotInstalled       = errors.New("skill is not installed")
	ErrNotGitOrigin       = errors.New("skill origin is not a git repository")
	ErrGitNotAvailable    = errors.New("git binary not available on PATH")
)

// InstallOptions controls the behaviour of Install* functions.
type InstallOptions struct {
	Force bool
}

// Origin records how a skill was installed so Update knows where to refresh from.
type Origin struct {
	Type        string    `json:"type"`         // "git", "url", "path", "registry"
	URL         string    `json:"url,omitempty"`
	Path        string    `json:"path,omitempty"`
	RegistryID  string    `json:"registry_id,omitempty"`
	Ref         string    `json:"ref,omitempty"`
	CommitSHA   string    `json:"commit_sha,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

// InstallResult describes what was installed.
type InstallResult struct {
	SkillID    string `json:"skill_id"`
	InstallDir string `json:"install_dir"`
	Origin     Origin `json:"origin"`
}

// LoadOrigin reads <installDir>/.sprout-origin.json.
func LoadOrigin(installDir string) (Origin, error) {
	data, err := os.ReadFile(filepath.Join(installDir, OriginMetadataFile))
	if err != nil {
		return Origin{}, fmt.Errorf("read origin: %w", err)
	}
	var origin Origin
	if err := json.Unmarshal(data, &origin); err != nil {
		return Origin{}, fmt.Errorf("parse origin: %w", err)
	}
	return origin, nil
}

// InstallFromPath copies a local file or directory into the skills dir.
func InstallFromPath(srcPath string, opts InstallOptions) ([]InstallResult, error) {
	absSrc, err := filepath.Abs(srcPath)
	if err != nil {
		return nil, fmt.Errorf("resolve source path: %w", err)
	}

	fi, err := os.Stat(absSrc)
	if err != nil {
		return nil, fmt.Errorf("stat source: %w", err)
	}

	if !fi.IsDir() {
		// src is a file (SKILL.md); parent dir becomes source dir
		return installFromFile(absSrc, opts)
	}

	return installFromDir(absSrc, opts)
}

func installFromFile(srcFile string, opts InstallOptions) ([]InstallResult, error) {
	content, err := os.ReadFile(srcFile)
	if err != nil {
		return nil, fmt.Errorf("read skill file: %w", err)
	}
	fm, err := parseSkillFrontmatter(string(content))
	if err != nil {
		return nil, err
	}

	skillID := fm.Name

	// Copy JUST the SKILL.md into a fresh tmpdir so we don't drag in
	// the user's parent directory contents (e.g. ~/Downloads).
	tmpDir, err := os.MkdirTemp("", "sprout-skill-src-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dstFile := filepath.Join(tmpDir, SkillFileName)
	if err := copyFile(srcFile, dstFile); err != nil {
		return nil, fmt.Errorf("copy skill file: %w", err)
	}

	origin := Origin{
		Type:        "path",
		Path:        srcFile,
		InstalledAt: time.Now(),
	}

	result, err := installSkill(tmpDir, skillID, origin, opts)
	if err != nil {
		return nil, err
	}
	return []InstallResult{result}, nil
}

func installFromDir(srcDir string, opts InstallOptions) ([]InstallResult, error) {
	skillMDPath := filepath.Join(srcDir, SkillFileName)
	content, err := os.ReadFile(skillMDPath)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}
	fm, err := parseSkillFrontmatter(string(content))
	if err != nil {
		return nil, err
	}

	skillID := fm.Name

	origin := Origin{
		Type:        "path",
		Path:        srcDir,
		InstalledAt: time.Now(),
	}

	result, err := installSkill(srcDir, skillID, origin, opts)
	if err != nil {
		return nil, err
	}
	return []InstallResult{result}, nil
}

// InstallFromGit clones a git repo and installs any SKILL.md skills found.
func InstallFromGit(ctx context.Context, gitURL, ref string, opts InstallOptions) ([]InstallResult, error) {
	if !gitAvailable() {
		return nil, ErrGitNotAvailable
	}

	tmpDir, err := os.MkdirTemp("", "sprout-skill-git-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	cloneArgs := []string{"clone", "--depth", "1"}
	if ref != "" {
		cloneArgs = append(cloneArgs, "--branch", ref)
	}
	cloneArgs = append(cloneArgs, gitURL, tmpDir)

	cmd := exec.CommandContext(ctx, "git", cloneArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git clone: %s: %w", string(out), err)
	}

	skills, err := findSkillMD(tmpDir)
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("no SKILL.md files found in cloned repository")
	}

	var results []InstallResult
	for _, skillMD := range skills {
		srcDir := filepath.Dir(skillMD)
		content, err := os.ReadFile(skillMD)
		if err != nil {
			return results, fmt.Errorf("read SKILL.md %s: %w", skillMD, err)
		}
		fm, err := parseSkillFrontmatter(string(content))
		if err != nil {
			return results, fmt.Errorf("parse SKILL.md %s: %w", skillMD, err)
		}

		skillID := fm.Name

		origin := Origin{
			Type:        "git",
			URL:         gitURL,
			Ref:         ref,
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

// Uninstall removes <skills_dir>/<skillID> entirely.
func Uninstall(skillID string) error {
	installDir, err := SkillInstallDir(skillID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(installDir); os.IsNotExist(err) {
		return ErrNotInstalled
	}
	return os.RemoveAll(installDir)
}

// Update refreshes an installed skill from its original source.
func Update(ctx context.Context, skillID string, opts InstallOptions) ([]InstallResult, error) {
	installDir, err := SkillInstallDir(skillID)
	if err != nil {
		return nil, err
	}
	origin, err := LoadOrigin(installDir)
	if err != nil {
		return nil, fmt.Errorf("load origin for %s: %w", skillID, err)
	}

	switch origin.Type {
	case "path":
		return nil, ErrNotGitOrigin

	case "git":
		if !gitAvailable() {
			return nil, ErrGitNotAvailable
		}
		cmd := exec.CommandContext(ctx, "git", "-C", installDir, "pull", "--ff-only")
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git pull: %s: %w", string(out), err)
		}
		return []InstallResult{
			{SkillID: skillID, InstallDir: installDir, Origin: origin},
		}, nil

	case "url":
		if origin.URL == "" {
			return nil, fmt.Errorf("origin URL is empty for %s", skillID)
		}
		return InstallFromURL(ctx, origin.URL, opts)

	case "registry":
		if origin.RegistryID == "" {
			return nil, fmt.Errorf("origin registry ID is empty for %s", skillID)
		}
		return InstallFromRegistry(ctx, origin.RegistryID, opts)

	default:
		return nil, fmt.Errorf("unknown origin type %q for %s", origin.Type, skillID)
	}
}

// --- Private helpers ---

func ensureSkillsDir() error {
	dir, err := DefaultSkillsDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}

func installSkill(srcDir, skillID string, origin Origin, opts InstallOptions) (InstallResult, error) {
	if err := validateSkillID(skillID); err != nil {
		return InstallResult{}, err
	}
	if err := ensureSkillsDir(); err != nil {
		return InstallResult{}, fmt.Errorf("ensure skills dir: %w", err)
	}

	skillsDir, err := DefaultSkillsDir()
	if err != nil {
		return InstallResult{}, err
	}
	installDir := filepath.Join(skillsDir, skillID)

	// Check if already installed
	if _, err := os.Stat(installDir); err == nil {
		if !opts.Force {
			return InstallResult{}, ErrAlreadyInstalled
		}
		// Force: remove existing
		if err := os.RemoveAll(installDir); err != nil {
			return InstallResult{}, fmt.Errorf("remove existing install: %w", err)
		}
	}

	// Copy source dir contents to install dir
	if err := copyDir(srcDir, installDir); err != nil {
		return InstallResult{}, fmt.Errorf("copy skill files: %w", err)
	}

	// Write origin metadata
	originData, err := json.MarshalIndent(origin, "", "  ")
	if err != nil {
		return InstallResult{}, fmt.Errorf("marshal origin: %w", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, OriginMetadataFile), originData, 0o644); err != nil {
		return InstallResult{}, fmt.Errorf("write origin file: %w", err)
	}

	return InstallResult{
		SkillID:    skillID,
		InstallDir: installDir,
		Origin:     origin,
	}, nil
}

// validateSkillID ensures a skillID is safe to use as a directory name.
// Mirrors the strict check in builtin.go's validSkillID but is duplicated
// here so install code does not depend on internal helpers.
func validateSkillID(id string) error {
	if id == "" {
		return fmt.Errorf("skill id is empty")
	}
	if id == "." || id == ".." {
		return fmt.Errorf("invalid skill id %q: reserved", id)
	}
	if strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("invalid skill id %q: must not contain path separators", id)
	}
	if filepath.IsAbs(id) {
		return fmt.Errorf("invalid skill id %q: must be relative", id)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("invalid skill id %q: must not contain '..'", id)
	}
	return nil
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func findSkillMD(root string) ([]string, error) {
	var results []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == SkillFileName {
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			results = append(results, abs)
		}
		return nil
	})
	return results, err
}

func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return nil
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
