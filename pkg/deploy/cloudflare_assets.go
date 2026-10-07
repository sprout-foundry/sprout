// Build-output collection for the Cloudflare adapters.
//
// A static deploy uploads the files under the build directory. They are read
// eagerly and carried as a bounded list, so the upload is a single
// deterministic request and a missing or unreadable build directory fails
// before anything is created on the account.

package deploy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxCloudflareAssetCount bounds how many files a single deploy uploads, so a
// misconfigured build directory (say, the project root) cannot turn into an
// unbounded upload.
const maxCloudflareAssetCount = 20000

// maxCloudflareAssetBytes bounds the size of one uploaded file.
const maxCloudflareAssetBytes = 25 << 20

// collectAssets reads the built output under dir into the file list a Pages
// deployment uploads. Paths are relative, slash-separated, and sorted so the
// upload is deterministic. An empty or missing directory is an error: a deploy
// with nothing to upload is a misconfiguration, not an empty deploy.
func collectAssets(dir string) ([]pagesAsset, error) {
	root := strings.TrimSpace(dir)
	if root == "" {
		return nil, errors.New("build directory must not be empty")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	// A root-scoped filesystem keeps reads inside the build tree: a symlink
	// swapped in between the walk and the read resolves under root, not
	// wherever it now points.
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", root, err)
	}
	defer func() { _ = rootFS.Close() }()

	var assets []pagesAsset
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if len(assets) >= maxCloudflareAssetCount {
			return fmt.Errorf("build output exceeds %d files", maxCloudflareAssetCount)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxCloudflareAssetBytes {
			return fmt.Errorf("file %s exceeds %d bytes", path, maxCloudflareAssetBytes)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// G122: read through an os.Root scoped to the build directory rather
		// than the walked path, so a symlink swapped in between the walk and
		// the read cannot escape the output tree.
		content, err := rootFS.ReadFile(rel)
		if err != nil {
			return err
		}
		assets = append(assets, pagesAsset{
			Path:    filepath.ToSlash(rel),
			Content: string(content),
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("%s contains no files to upload", root)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	return assets, nil
}
