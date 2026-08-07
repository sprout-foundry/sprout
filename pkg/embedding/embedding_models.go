package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

type EmbeddingManager struct {
	mu            sync.Mutex
	provider      EmbeddingProvider
	store         VectorStore
	indexMgr      *IndexManager
	initialized   atomic.Bool
	building      bool
	initError     error
	config        *configuration.EmbeddingIndexConfig
	workspaceRoot string

	threshold  float32
	maxResults int

	convoStore *ConversationStore
	indexDir   string

	onnxRuntime    *ONNXRuntime
	providerShared bool

	closeChan chan struct{}

	cachedProvider *cachedProvider

	sharedKey      string
	autoBuildOnce  sync.Once
}

type BuildResult struct {
	Stats *IndexStats
	Err   error
}

func NewEmbeddingManager(cfg *configuration.EmbeddingIndexConfig, workspaceRoot string) *EmbeddingManager {
	return &EmbeddingManager{
		config:        cfg,
		workspaceRoot: workspaceRoot,
	}
}

func (m *EmbeddingManager) SetForTesting(provider EmbeddingProvider, store VectorStore, indexMgr *IndexManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = provider
	m.cachedProvider = newCachedProvider(provider)
	m.store = store
	m.indexMgr = indexMgr
	m.initialized.Store(true)
	m.indexDir = resolveIndexDirFromConfig(m.config, m.workspaceRoot)
}

func (m *EmbeddingManager) IsInitialized() bool {
	return m.initialized.Load()
}

type IndexReadiness struct {
	Initialized bool
	Building    bool
	Records     int
}

func (r IndexReadiness) CanAnswerQueries() bool {
	return r.Initialized && r.Records > 0
}

func (m *EmbeddingManager) Readiness() IndexReadiness {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := IndexReadiness{
		Initialized: m.initialized.Load(),
		Building:    m.building,
	}
	if m.store != nil {
		r.Records = m.store.Size()
	}
	return r
}

func (m *EmbeddingManager) IsBuilding() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.building
}

func (m *EmbeddingManager) InitError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.initError
}

func (m *EmbeddingManager) IndexSize() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store != nil {
		return m.store.Size()
	}
	return 0
}

func (m *EmbeddingManager) ModelHash() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.provider == nil {
		return ""
	}
	return m.provider.ModelHash()
}

func (m *EmbeddingManager) closeCh() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closeChan == nil {
		m.closeChan = make(chan struct{})
	}
	return m.closeChan
}

func (m *EmbeddingManager) CloseNotify() <-chan struct{} {
	return m.closeCh()
}

func (m *EmbeddingManager) snapshotIndexMgr() (*IndexManager, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized.Load() {
		return nil, fmt.Errorf("embedding: manager not initialized")
	}
	return m.indexMgr, nil
}

func (m *EmbeddingManager) snapshotQueryParams() (float32, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.threshold, m.maxResults
}

func (m *EmbeddingManager) SemanticSearchThreshold() float32 {
	return DefaultCodeModelSemanticSearchThreshold
}

func (m *EmbeddingManager) RelatedCodeThreshold() float32 {
	return DefaultCodeModelRelatedThreshold
}

func resolveIndexDirFromConfig(cfg *configuration.EmbeddingIndexConfig, workspaceRoot string) string {
	indexDir := ""
	if cfg != nil {
		indexDir = cfg.IndexDir
	}
	if indexDir == "" {
		indexDir = resolveIndexDir(workspaceRoot)
	}
	return indexDir
}

func DefaultIndexDir(workspaceRoot string) string {
	return resolveIndexDir(workspaceRoot)
}

// resolveIndexDir resolves the index directory under the data root, scoped
// per-workspace. The workspace slug is load-bearing: without it, every manager
// in a process shares one index, and the daemon's per-session managers would
// clobber each other's manifests on every build.
func resolveIndexDir(workspaceRoot string) string {
	dataDir, err := envutil.DataDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share", "sprout")
	}
	base := filepath.Join(dataDir, "embeddings")
	if slug := workspaceSlug(workspaceRoot); slug != "" {
		return filepath.Join(base, slug)
	}
	return base
}

func workspaceSlug(workspaceRoot string) string {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return ""
	}

	resolved := workspaceRoot
	if abs, err := filepath.Abs(resolved); err == nil {
		resolved = abs
	}
	if evaled, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = evaled
	}
	resolved = filepath.Clean(resolved)

	sum := sha256.Sum256([]byte(resolved))
	name := sanitizeSlugName(filepath.Base(resolved))
	return name + "-" + hex.EncodeToString(sum[:4])
}

func sanitizeSlugName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "workspace"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

func (m *EmbeddingManager) createONNXProvider(ctx context.Context) (EmbeddingProvider, *ONNXRuntime, error) {
	return acquireSharedJinaProvider(ctx, DefaultModelDir(), JinaCodeV2Config())
}
