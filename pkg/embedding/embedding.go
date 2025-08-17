package embedding

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/workspaceinfo"
)

// CodeEmbedding represents a vector embedding for a code entity (file, function, class, etc.)
type CodeEmbedding struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"` // "file", "function", "class"
	Path        string    `json:"path"`
	Name        string    `json:"name"`
	Vector      []float64 `json:"vector"`
	TokenCount  int       `json:"token_count"`
	LastUpdated time.Time `json:"last_updated"`
}

// VectorDB represents an in-memory vector database for code embeddings
type VectorDB struct {
	embeddings map[string]*CodeEmbedding
	mu         sync.RWMutex
}

// NewVectorDB creates a new vector database
func NewVectorDB() *VectorDB {
	return &VectorDB{
		embeddings: make(map[string]*CodeEmbedding),
	}
}

// GetEmbeddingFilePath returns the file path for a given embedding ID.
func GetEmbeddingFilePath(id string) string {
	encodedID := base64.URLEncoding.EncodeToString([]byte(id))
	return filepath.Join(".ledit", "embeddings", encodedID+".json")
}

// LoadEmbedding loads a single CodeEmbedding from a file.
func LoadEmbedding(id string) (*CodeEmbedding, error) {
	filePath := GetEmbeddingFilePath(id)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var emb CodeEmbedding
	if err := json.Unmarshal(data, &emb); err != nil {
		return nil, fmt.Errorf("failed to unmarshal embedding from %s: %w", filePath, err)
	}
	return &emb, nil
}

// SaveEmbedding saves a single CodeEmbedding to a file.
func SaveEmbedding(emb *CodeEmbedding) error {
	filePath := GetEmbeddingFilePath(emb.ID)
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory for embedding %s: %w", filePath, err)
	}
	data, err := json.MarshalIndent(emb, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal embedding for %s: %w", emb.ID, err)
	}
	return os.WriteFile(filePath, data, 0644)
}

// Add adds an embedding to the in-memory database.
func (vdb *VectorDB) Add(embedding *CodeEmbedding) {
	vdb.mu.Lock()
	defer vdb.mu.Unlock()

	vdb.embeddings[embedding.ID] = embedding
}

// Get retrieves an embedding by ID.
func (vdb *VectorDB) Get(id string) (*CodeEmbedding, bool) {
	vdb.mu.RLock()
	defer vdb.mu.RUnlock()

	emb, exists := vdb.embeddings[id]
	return emb, exists
}

// Remove removes an embedding from the database and deletes its file from disk.
func (vdb *VectorDB) Remove(id string) error {
	vdb.mu.Lock()
	defer vdb.mu.Unlock()

	delete(vdb.embeddings, id)
	filePath := GetEmbeddingFilePath(id)
	err := os.Remove(filePath)
	if err != nil && os.IsNotExist(err) {
		return nil // File already doesn't exist, consider it removed successfully
	}
	return err
}

// GetAll returns all embeddings, loading them from disk if not already in memory.
func (vdb *VectorDB) GetAll() ([]*CodeEmbedding, error) {
	vdb.mu.Lock() // Use Lock since we might be modifying the map
	defer vdb.mu.Unlock()

	// Clear existing in-memory embeddings to ensure fresh load
	vdb.embeddings = make(map[string]*CodeEmbedding)

	embeddingsDir := filepath.Join(".ledit", "embeddings")
	files, err := os.ReadDir(embeddingsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*CodeEmbedding{}, nil // Directory doesn't exist, no embeddings
		}
		return nil, fmt.Errorf("failed to read embeddings directory: %w", err)
	}

	var allEmbeddings []*CodeEmbedding
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			encodedID := strings.TrimSuffix(file.Name(), ".json")
			// Decode the base64 filename to get the actual ID
			idBytes, err := base64.URLEncoding.DecodeString(encodedID)
			if err != nil {
				fmt.Printf("Warning: failed to decode embedding filename %s: %v\n", encodedID, err)
				continue
			}
			id := string(idBytes)

			emb, err := LoadEmbedding(id)
			if err != nil {
				// Log error but continue with other embeddings
				fmt.Printf("Warning: failed to load embedding %s: %v\n", id, err)
				continue
			}
			vdb.embeddings[emb.ID] = emb // Add to in-memory cache
			allEmbeddings = append(allEmbeddings, emb)
		}
	}

	return allEmbeddings, nil
}

// Search finds the top K most similar embeddings to the query vector
func (vdb *VectorDB) Search(queryVector []float64, topK int) ([]*CodeEmbedding, []float64, error) {
	vdb.mu.RLock()
	defer vdb.mu.RUnlock()

	if len(vdb.embeddings) == 0 {
		return nil, nil, nil
	}

	type result struct {
		embedding *CodeEmbedding
		score     float64
	}

	results := make([]result, 0, len(vdb.embeddings))

	for _, emb := range vdb.embeddings {
		score, err := llm.CosineSimilarity(queryVector, emb.Vector)
		if err != nil {
			// Skip embeddings that can't be compared
			continue
		}
		results = append(results, result{embedding: emb, score: score})
	}

	// Sort by score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// Take top K
	if topK > len(results) {
		topK = len(results)
	}

	topResults := results[:topK]
	embeddings := make([]*CodeEmbedding, len(topResults))
	scores := make([]float64, len(topResults))

	for i, res := range topResults {
		embeddings[i] = res.embedding
		scores[i] = res.score
	}

	return embeddings, scores, nil
}

// GenerateFileEmbedding generates an embedding for a file
func GenerateFileEmbedding(filePath string, fileInfo workspaceinfo.WorkspaceFileInfo, cfg *config.Config) (*CodeEmbedding, error) {
	// For files, we'll use a combination of the file path, summary, and exports for the embedding
	// This gives us semantic information about the file's purpose and contents
	textForEmbedding := fmt.Sprintf(`File: %s
Summary: %s
Exports: %s`,
		filePath, fileInfo.Summary, fileInfo.Exports)

	vector, err := llm.GenerateEmbedding(textForEmbedding, cfg.EmbeddingModel)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding for file %s: %w", filePath, err)
	}

	return &CodeEmbedding{
		ID:          fmt.Sprintf("file:%s", filePath),
		Type:        "file",
		Path:        filePath,
		Name:        filepath.Base(filePath),
		Vector:      vector,
		TokenCount:  fileInfo.TokenCount,
		LastUpdated: time.Now(),
	}, nil
}

// GenerateWorkspaceEmbeddings generates embeddings for all files in a workspace.
// It uses a caching mechanism to avoid re-generating embeddings for unchanged files.
func GenerateWorkspaceEmbeddings(workspace workspaceinfo.WorkspaceFile, db *VectorDB, cfg *config.Config) error {
	// Get all existing embeddings from the database (which now loads from disk)
	existingEmbeddings, err := db.GetAll()
	if err != nil {
		return fmt.Errorf("failed to get existing embeddings: %w", err)
	}

	// Create a map for quick lookup of existing embeddings by file path
	existingEmbeddingsMap := make(map[string]*CodeEmbedding)
	for _, emb := range existingEmbeddings {
		existingEmbeddingsMap[emb.Path] = emb
	}

	// Track files that are no longer in the workspace
	var toRemove []string
	for filePath, emb := range existingEmbeddingsMap {
		if _, exists := workspace.Files[filePath]; !exists {
			toRemove = append(toRemove, emb.ID)
		}
	}

	// Remove embeddings for files that no longer exist
	for _, id := range toRemove {
		if err := db.Remove(id); err != nil {
			fmt.Printf("Warning: failed to remove embedding for %s: %v\n", id, err)
		}
	}

	// Generate or update embeddings for current workspace files
	var filesToProcess []struct {
		filePath string
		fileInfo workspaceinfo.WorkspaceFileInfo
	}

	// Collect files that need processing
	for filePath, fileInfo := range workspace.Files {
		// Check if embedding exists and is up-to-date
		if existingEmb, exists := existingEmbeddingsMap[filePath]; exists {
			fileModTime, err := os.Stat(filePath)
			if err == nil && !fileModTime.ModTime().After(existingEmb.LastUpdated) {
				// Embedding is up-to-date, add to in-memory DB and skip generation
				db.Add(existingEmb) // Add back to in-memory map
				continue
			}
		}

		filesToProcess = append(filesToProcess, struct {
			filePath string
			fileInfo workspaceinfo.WorkspaceFileInfo
		}{filePath, fileInfo})
	}

	// Process files in batches to avoid rate limits
	batchSize := cfg.EmbeddingBatchSize
	if batchSize <= 0 {
		batchSize = 5 // Default fallback
	}

	for i := 0; i < len(filesToProcess); i += batchSize {
		end := i + batchSize
		if end > len(filesToProcess) {
			end = len(filesToProcess)
		}
		batch := filesToProcess[i:end]

		// Process batch concurrently with limited concurrency
		var wg sync.WaitGroup
		sem := make(chan struct{}, cfg.MaxConcurrentRequests)
		if sem == nil {
			sem = make(chan struct{}, 3) // Default fallback
		}

		for _, fileData := range batch {
			wg.Add(1)
			go func(fd struct {
				filePath string
				fileInfo workspaceinfo.WorkspaceFileInfo
			}) {
				defer wg.Done()
				sem <- struct{}{} // Acquire semaphore
				defer func() { <-sem }() // Release semaphore

				// Generate new embedding
				embedding, err := GenerateFileEmbedding(fd.filePath, fd.fileInfo, cfg)
				if err != nil {
					fmt.Printf("Warning: failed to generate embedding for file %s: %v\n", fd.filePath, err)
					return
				}
				db.Add(embedding)
				if err := SaveEmbedding(embedding); err != nil {
					fmt.Printf("Warning: failed to add embedding for file %s to DB: %v\n", fd.filePath, err)
				}
			}(fileData)
		}

		// Wait for batch to complete
		wg.Wait()

		// Add delay between batches to avoid rate limits
		if cfg.RequestDelayMs > 0 && i+batchSize < len(filesToProcess) {
			time.Sleep(time.Duration(cfg.RequestDelayMs) * time.Millisecond)
		}
	}

	return nil
}

// SearchRelevantFiles finds the most relevant files for a given query
func SearchRelevantFiles(query string, db *VectorDB, topK int, cfg *config.Config) ([]*CodeEmbedding, []float64, error) {
	// Generate embedding for the query
	queryVector, err := llm.GenerateEmbedding(query, cfg.EmbeddingModel)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate embedding for query: %w", err)
	}

	// Search for relevant file embeddings
	return db.Search(queryVector, topK)
}
