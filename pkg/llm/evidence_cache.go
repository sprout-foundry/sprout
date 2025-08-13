package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type EvidenceEntry struct {
	Tool     string `json:"tool"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	FilePath string `json:"file_path,omitempty"`
	FileHash string `json:"file_hash,omitempty"`
	Updated  int64  `json:"updated_at"`
}

type EvidenceCache struct {
	Entries map[string]EvidenceEntry `json:"entries"`
}

func evidenceCachePath() string {
	return filepath.Join(".ledit", "evidence_cache.json")
}

func LoadEvidenceCache() *EvidenceCache {
	_ = os.MkdirAll(".ledit", 0755)
	path := evidenceCachePath()
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return &EvidenceCache{Entries: map[string]EvidenceEntry{}}
	}
	var c EvidenceCache
	if json.Unmarshal(b, &c) != nil || c.Entries == nil {
		return &EvidenceCache{Entries: map[string]EvidenceEntry{}}
	}
	return &c
}

func (c *EvidenceCache) Save() error {
	_ = os.MkdirAll(".ledit", 0755)
	path := evidenceCachePath()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

func (c *EvidenceCache) Get(tool, key string) (EvidenceEntry, bool) {
	entry, ok := c.Entries[tool+"|"+key]
	return entry, ok
}

func (c *EvidenceCache) Put(e EvidenceEntry) {
	if c.Entries == nil {
		c.Entries = map[string]EvidenceEntry{}
	}
	// Cap cache size to avoid unbounded growth (simple oldest eviction)
	const maxEntries = 300
	if len(c.Entries) >= maxEntries {
		// evict oldest 10
		type kv struct {
			K string
			V EvidenceEntry
		}
		var arr []kv
		for k, v := range c.Entries {
			arr = append(arr, kv{k, v})
		}
		sort.Slice(arr, func(i, j int) bool { return arr[i].V.Updated < arr[j].V.Updated })
		for i := 0; i < 10 && i < len(arr); i++ {
			delete(c.Entries, arr[i].K)
		}
	}
	c.Entries[e.Tool+"|"+e.Key] = e
}

func ComputeFileHash(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sha := sha256.Sum256(b)
	return hex.EncodeToString(sha[:]), nil
}

func NowUnix() int64 { return time.Now().Unix() }
