package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// Runtime-writable layer of the vision capability resolver (SP-140 Phase 2).
//
// When a provider rejects image parts with a capability-shaped 4xx, the
// rejection is classified and recorded here; a verified success records
// known-true. The file is the override mechanism: hand-edit or delete it to
// correct a wrong entry — no config surface exists beyond the file itself.

const (
	visionCapFileName          = "vision-capabilities.json"
	positiveVisionCapTTL       = 30 * 24 * time.Hour // providers rarely remove vision
	negativeVisionCapTTL       = 7 * 24 * time.Hour  // providers often add vision
	visionCapProviderSeparator = "/"
)

// VisionCapEntry is one recorded observation for a provider/model pair.
type VisionCapEntry struct {
	AcceptsImages bool      `json:"accepts_images"`
	VerifiedAt    time.Time `json:"verified_at"`
}

// visionCapFile is the on-disk shape: provider/model → observation.
type visionCapFile map[string]VisionCapEntry

// visionCapCache is a process-wide memo over the JSON file so the send
// path pays one stat/read per resolution at most.
type visionCapCache struct {
	mu      sync.Mutex
	loaded  bool
	modTime time.Time
	data    visionCapFile
}

var globalVisionCapCache = &visionCapCache{}

// visionCapPath returns the state-dir file path (not created).
func visionCapPath() (string, error) {
	dir, err := envutil.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, visionCapFileName), nil
}

// load returns the memoized file contents, re-reading when the file changed.
// Missing file → empty map, no error (nothing learned yet).
func (c *visionCapCache) load() visionCapFile {
	c.mu.Lock()
	defer c.mu.Unlock()
	path, err := visionCapPath()
	if err != nil {
		return visionCapFile{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return visionCapFile{}
	}
	if c.loaded && info.ModTime().Equal(c.modTime) {
		return c.data
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return visionCapFile{}
	}
	data := visionCapFile{}
	if json.Unmarshal(raw, &data) != nil {
		return visionCapFile{} // corrupt file behaves as unlearned, never fatal
	}
	c.loaded = true
	c.modTime = info.ModTime()
	c.data = data
	return c.data
}

// visionCapKey normalizes provider/model identity for file keys.
func visionCapKey(provider, model string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + visionCapProviderSeparator + strings.ToLower(strings.TrimSpace(model))
}

// LearnedVisionAcceptance returns the runtime observation for a
// provider/model pair, or nil when nothing (or nothing fresh) is recorded.
// Positive entries live 30d, negative 7d.
func LearnedVisionAcceptance(provider, model string) *bool {
	for key, entry := range globalVisionCapCache.load() {
		if key != visionCapKey(provider, model) {
			continue
		}
		ttl := positiveVisionCapTTL
		if !entry.AcceptsImages {
			ttl = negativeVisionCapTTL
		}
		if time.Since(entry.VerifiedAt) > ttl {
			return nil
		}
		out := entry.AcceptsImages
		return &out
	}
	return nil
}

// RecordVisionAcceptance writes an observation to the state-dir file.
// Best-effort: write failures are silent — capability learning must never
// break a chat turn.
func RecordVisionAcceptance(provider, model string, accepts bool) {
	c := globalVisionCapCache
	c.mu.Lock()
	defer c.mu.Unlock()

	path, err := visionCapPath()
	if err != nil {
		return
	}
	data := c.data
	if c.loaded {
		// Refresh from disk under the lock in case another process wrote.
		if raw, err := os.ReadFile(path); err == nil {
			fresh := visionCapFile{}
			if json.Unmarshal(raw, &fresh) == nil {
				data = fresh
			}
		}
	} else {
		data = visionCapFile{}
	}

	data[visionCapKey(provider, model)] = VisionCapEntry{
		AcceptsImages: accepts,
		VerifiedAt:    time.Now().UTC(),
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return
	}

	c.loaded = true
	if info, err := os.Stat(path); err == nil {
		c.modTime = info.ModTime()
	}
	c.data = data
}

// IsVisionCapabilityRejection classifies a provider error: true when the
// failure looks like a model/provider refusing image input. Conservative by
// design — 400-class status text plus modality keywords only; network
// errors, 5xx, and auth failures never classify as capability rejections,
// so transient problems cannot poison the cache.
func IsVisionCapabilityRejection(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "http 4") {
		return false
	}
	for _, kw := range []string{"image", "modalit", "vision", "multimodal", "not supported", "unsupported"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// MessagesHaveImages reports whether any message carries image parts.
func MessagesHaveImages(messages []Message) bool {
	for i := range messages {
		if len(messages[i].Images) > 0 {
			return true
		}
	}
	return false
}

// StripImagesWithNote returns a copy of messages with all image parts
// removed and a bracketed note appended to each affected message's text,
// so the model knows content was withheld rather than never sent. The
// input slice and its messages are not modified.
func StripImagesWithNote(messages []Message) []Message {
	out := make([]Message, len(messages))
	copy(out, messages)
	for i := range out {
		if len(out[i].Images) == 0 {
			continue
		}
		out[i].Images = nil
		note := "\n\n[image content withheld: this model declined image input]"
		if strings.Contains(out[i].Content, note) {
			continue
		}
		out[i].Content += note
	}
	return out
}
