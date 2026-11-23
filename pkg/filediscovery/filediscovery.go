// Package filediscovery: core types and entry points (split from filediscovery.go)

package filediscovery

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// Configurable constants for performance limits
const (
	// BuildProjectFolderTimeout is the absolute maximum time for project folder structure building
	BuildProjectFolderTimeout = 30 * time.Second

	// CacheDuration is how long project folder structure results are cached
	CacheDuration = 5 * time.Minute

	// ProgressEmitInterval controls how often progress events are emitted during file walk
	ProgressEmitInterval = 1000 // emit every N files

	// MaxDepth limits directory traversal depth in BuildProjectFolderStructure
	MaxDepth = 20

	// LargeProjectFolderThreshold is the file count above which limits apply
	LargeProjectFolderThreshold = 1000
)

// FileDiscovery provides common file discovery and analysis functionality
type FileDiscovery struct {
	config *configuration.Config
	logger *utils.Logger

	// cache for project folder structure results
	cacheMu  sync.RWMutex
	cacheMap map[string]*cacheEntry
}

// cacheEntry holds a cached project folder structure result
type cacheEntry struct {
	data      *ProjectFolderInfo
	modTime   time.Time
	createdAt time.Time
}

// NewFileDiscovery creates a new file discovery instance
func NewFileDiscovery(cfg *configuration.Config, logger *utils.Logger) *FileDiscovery {
	return &FileDiscovery{
		config:   cfg,
		logger:   logger,
		cacheMap: make(map[string]*cacheEntry),
	}
}

// FileResult represents the result of a file discovery operation
type FileResult struct {
	Files        []string
	Error        error
	Duration     time.Duration
	Method       string
	TotalFiles   int
	MatchedFiles int
}

// ProjectFolderInfo represents project folder information
type ProjectFolderInfo struct {
	ProjectType string
	AllFiles    []string
	FilesByDir  map[string][]string
	Error       error
	RootDir     string
}

// DiscoverFilesRobust uses multiple strategies to find relevant files
func (fd *FileDiscovery) DiscoverFilesRobust(userIntent string, options *DiscoveryOptions) *FileResult {
	startTime := time.Now()

	if options == nil {
		options = &DiscoveryOptions{
			MaxFiles:   50,
			UseSymbols: true,
			UseShell:   true,
		}
	}

	// Fallback to shell-based discovery
	if options.UseShell {
		if result := fd.discoverWithShell(userIntent, options); result != nil && len(result.Files) > 0 {
			result.Duration = time.Since(startTime)
			result.Method = "shell"
			return result
		}
	}

	// Final fallback to basic file listing
	result := fd.discoverBasic(options)
	result.Duration = time.Since(startTime)
	result.Method = "basic"

	return result
}

// DiscoveryOptions configures file discovery behavior
type DiscoveryOptions struct {
	MaxFiles      int
	UseSymbols    bool
	UseShell      bool
	IncludeHidden bool
	ExcludeDirs   []string
	IncludeExts   []string
	ExcludeExts   []string
	RootPath      string
}

// discoverWithShell uses shell commands to find files
func (fd *FileDiscovery) discoverWithShell(userIntent string, options *DiscoveryOptions) *FileResult {
	terms := fd.extractSearchTerms(userIntent)
	if len(terms) == 0 {
		return nil
	}

	// Build project folder info for shell commands
	projectFolderInfo := &ProjectFolderInfo{
		ProjectType: "other",
		RootDir:     options.RootPath,
	}
	if projectFolderInfo.RootDir == "" {
		projectFolderInfo.RootDir, _ = filepath.Abs(".")
	}

	joined := strings.Join(terms, " ")
	found := fd.findFilesUsingShellCommands(joined, projectFolderInfo)

	result := fd.applyFiltersAndLimits(found, options)
	result.TotalFiles = len(found)

	return result
}
