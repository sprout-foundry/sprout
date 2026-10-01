//go:build !js

package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// critiqueCacheFilename is the sidecar a cached render writes next to its PNG:
// design/.cache/renders/<stem>.cache.json. The PNG stays a plain
// provenance-headed image (SP-140 invariant 2); the sidecar holds the machine
// -readable cache key, so a cache lookup never has to parse the provenance
// banner out of image bytes.
const critiqueCacheFilenameSuffix = ".cache.json"

// critiqueCacheEntry is the sidecar record for one cached render. It is
// written atomically alongside the PNG and read on the next run.
type critiqueCacheEntry struct {
	// SourceHash is the SHA-256 (hex) of the render source bytes — the §4e
	// content hash. For a mermaid flow it is the hash of the .mmd content; for
	// an SVG/HTML screen it is the hash of the source file content.
	SourceHash string `json:"sourceHash"`
	// RenderMaterial is the canonical render material (viewport + rubric)
	// folded into the cache key, so a re-render with a different viewport or
	// rubric under the same path does not return a stale critique.
	RenderMaterial string `json:"renderMaterial"`
	// Artifact is the workspace-relative PNG path this entry describes.
	Artifact string `json:"artifact"`
	// Generated is when the cached render was produced (RFC3339 UTC).
	Generated string `json:"generated"`
}

// critiqueContentHash is the §4e content hash of a render source: the SHA-256
// of the source bytes, hex-encoded. It is the same hash the webui sidecar uses
// in spirit — a content-addressed digest of the render input — so an unchanged
// screen produces an unchanged key regardless of file mtime, and the hash can
// be recorded in the provenance header for a consumer to verify.
func critiqueContentHash(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}

// critiqueRenderMaterial canonically describes everything besides the source
// content that can change the rendered pixels or the critique: the viewport
// and the rubric/instruction. Two runs whose source content is identical but
// whose viewport or rubric differ are *not* interchangeable — a re-render at a
// new viewport (or under a different rubric) must not return a stale critique —
// so the material is folded into the cache key and any change is a cache miss.
func critiqueRenderMaterial(viewOpts RenderInputOptions, instruction string) string {
	return fmt.Sprintf("viewport=%sx%s;rubric=%s",
		trimFloat(viewOpts.ViewportWidth), trimFloat(viewOpts.ViewportHeight),
		strings.Join(strings.Fields(instruction), " "))
}

// critiqueCacheKey composes the artifact's cache key from the source content
// hash and the render material, so a consumer (and the sidecar) has one string
// that identifies "these pixels from this source at this viewport under this
// rubric".
func critiqueCacheKey(sourceHash, material string) string {
	sum := sha256.Sum256([]byte(sourceHash + "\x00" + material))
	return hex.EncodeToString(sum[:])
}

// critiqueCachePath is the sidecar path for a target's artifact, alongside the
// PNG under design/.cache/renders/.
func critiqueCachePath(artifactPath string) string {
	return artifactPath + critiqueCacheFilenameSuffix
}

// readCritiqueSourceBytes reads the bytes the §4e content hash is derived from:
// the render source itself. For a mermaid flow that is the .mmd file content
// (the HTML page is generated from it and is not the source of truth); for an
// SVG/HTML screen it is the source file content. Reading goes through the
// workspace-safe resolver, so it is subject to the same Gate-1 checks as any
// other read.
func readCritiqueSourceBytes(ctx context.Context, t critiqueTarget) ([]byte, error) {
	rel := firstNonEmpty(t.Source, t.Label)
	if rel == "" {
		return nil, errors.New("no render source to hash")
	}
	data, err := readDesignSource(ctx, rel)
	if err != nil {
		return nil, fmt.Errorf("cannot read render source %s: %w", rel, err)
	}
	return data, nil
}

// loadCachedCritiqueArtifact returns a reusable cached render for artifactPath
// when one exists whose sidecar matches cacheKey, so the caller can skip the
// browser render entirely (§4e). The PNG is re-attached through the SP-137 path
// and the vision critique is run against the cached pixels, so a cache hit
// produces the same result as a fresh render would — the only thing skipped is
// the rasterization.
//
// A missing/corrupt/uncertain cache is a miss, never an error: the caller then
// renders normally. That keeps a stale or hand-edited cache from breaking a
// critique.
func loadCachedCritiqueArtifact(ctx context.Context, artifactPath, cacheKey string) (cachedCritiqueArtifact, bool) {
	sidecarPath := critiqueCachePath(artifactPath)
	absSidecar, err := filesystem.SafeResolvePathWithBypass(ctx, sidecarPath)
	if err != nil {
		return cachedCritiqueArtifact{}, false
	}
	raw, err := os.ReadFile(absSidecar)
	if err != nil {
		return cachedCritiqueArtifact{}, false
	}
	var entry critiqueCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return cachedCritiqueArtifact{}, false
	}
	if entry.SourceHash == "" || entry.RenderMaterial == "" {
		return cachedCritiqueArtifact{}, false
	}
	// A sidecar naming a different artifact is not this artifact's cache entry
	// (e.g. a stale copy left behind by a rename): treat it as a miss.
	if entry.Artifact != "" && entry.Artifact != artifactPath {
		return cachedCritiqueArtifact{}, false
	}
	if critiqueCacheKey(entry.SourceHash, entry.RenderMaterial) != cacheKey {
		return cachedCritiqueArtifact{}, false
	}

	absPNG, err := filesystem.SafeResolvePathWithBypass(ctx, artifactPath)
	if err != nil {
		return cachedCritiqueArtifact{}, false
	}
	pngWithProvenance, readErr := os.ReadFile(absPNG)
	if readErr != nil {
		return cachedCritiqueArtifact{}, false
	}
	attachment, ok := buildRenderAttachment(ctx, absPNG)
	if !ok {
		return cachedCritiqueArtifact{}, false
	}
	return cachedCritiqueArtifact{
		attachment: attachment,
		artifact: critiqueArtifact{
			Target:     "",
			Path:       artifactPath,
			Provenance: extractProvenanceBanner(string(pngWithProvenance)),
			Cached:     true,
		},
	}, true
}

// extractProvenanceBanner pulls the textual provenance block back out of a
// cached artifact's bytes, so a cache hit reports the same provenance a fresh
// render would (the banner is appended after IEND and ignored by PNG decoders).
// A missing/malformed banner yields "" rather than an error: the artifact is
// still usable.
func extractProvenanceBanner(raw string) string {
	idx := strings.Index(raw, provenanceHeaderPrefix+"\n")
	if idx < 0 {
		return ""
	}
	body := raw[idx+len(provenanceHeaderPrefix)+1:]
	end := strings.Index(body, provenanceHeaderTerminator)
	if end < 0 {
		return ""
	}
	return strings.TrimSuffix(body[:end], "\n")
}

// cachedCritiqueArtifact is the reusable part of a cache hit: the SP-137
// attachment re-derived from the cached PNG plus the artifact descriptor.
type cachedCritiqueArtifact struct {
	attachment ToolResult
	artifact   critiqueArtifact
}

// writeCritiqueCache records the sidecar that makes a future run's cache lookup
// possible. It is best-effort: a failure to write the sidecar (or hand back a
// usable PNG) is reported so the caller can skip the cache but must never fail
// the critique, which already has its rendered pixels.
func writeCritiqueCache(ctx context.Context, artifactPath, cacheKey, sourceHash, material string) error {
	abs, err := filesystem.SafeResolvePathForWriteWithBypass(ctx, critiqueCachePath(artifactPath))
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		return mkErr
	}
	entry := critiqueCacheEntry{
		SourceHash:     sourceHash,
		RenderMaterial: material,
		Artifact:       artifactPath,
		Generated:      time.Now().UTC().Format(time.RFC3339),
	}
	data, jsonErr := json.MarshalIndent(entry, "", "  ")
	if jsonErr != nil {
		return jsonErr
	}
	return os.WriteFile(abs, append(data, '\n'), 0o644)
}
