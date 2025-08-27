package agent

import (
	"github.com/alantheprice/ledit/pkg/prompts"
	"sync"
)

// PromptCache provides caching for frequently used prompts to improve performance
type PromptCache struct {
	cache map[string]string
	mu    sync.RWMutex
}

var (
	globalPromptCache *PromptCache
	cacheOnce         sync.Once
)

// GetPromptCache returns the global prompt cache instance
func GetPromptCache() *PromptCache {
	cacheOnce.Do(func() {
		globalPromptCache = &PromptCache{
			cache: make(map[string]string),
		}
	})
	return globalPromptCache
}

// GetCachedPrompt retrieves a prompt from cache or loads it
func (pc *PromptCache) GetCachedPrompt(filename string) (string, error) {
	// Try cache first
	pc.mu.RLock()
	content, exists := pc.cache[filename]
	pc.mu.RUnlock()

	if exists {
		return content, nil
	}

	// Load from disk
	pm := prompts.GetPromptManager()
	content, err := pm.LoadPrompt(filename)
	if err != nil {
		return "", err
	}

	// Cache for future use
	pc.mu.Lock()
	pc.cache[filename] = content
	pc.mu.Unlock()

	return content, nil
}

// GetCachedPromptWithFallback gets a cached prompt with inline fallback
func (pc *PromptCache) GetCachedPromptWithFallback(filename string, fallback string) string {
	content, err := pc.GetCachedPrompt(filename)
	if err != nil {
		return fallback
	}
	return content
}

// WarmCache preloads commonly used prompts
func (pc *PromptCache) WarmCache() {
	commonPrompts := []string{
		"agent_todo_creation_system_optimized.txt",
		"agent_todo_creation_user_optimized.txt",
		"base_code_editing_optimized.txt",
		"base_code_editing_quality_enhanced.txt",
		"commit_message_system.txt",
		"code_generation_system_optimized.txt",
		"code_generation_system_quality_enhanced.txt",
		"code_review_system_optimized.txt",
		"code_review_system_quality_enhanced.txt",
		"interactive_code_generation_quality_enhanced.txt",
	}

	for _, prompt := range commonPrompts {
		go func(p string) {
			_, _ = pc.GetCachedPrompt(p) // Ignore errors during warm-up
		}(prompt)
	}
}
