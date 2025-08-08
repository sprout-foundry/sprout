package embedding

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

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

// Load loads embeddings from a file
func (vdb *VectorDB) Load(filepath string) error {
	vdb.mu.Lock()
	defer vdb.mu.Unlock()

	data, err := os.ReadFile(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, initialize empty embeddings map
			vdb.embeddings = make(map[string]*CodeEmbedding)
			return nil
		}
		return err
	}

	if len(data) == 0 {
		vdb.embeddings = make(map[string]*CodeEmbedding)
		return nil
	}

	return json.Unmarshal(data, &vdb.embeddings)
}

// Save saves embeddings to a file
func (vdb *VectorDB) Save(filePath string) error {
	vdb.mu.RLock()
	defer vdb.mu.RUnlock()

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	data, err := json.Marshal(vdb.embeddings)
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// Add adds an embedding to the database
func (vdb *VectorDB) Add(embedding *CodeEmbedding) {
	vdb.mu.Lock()
	defer vdb.mu.Unlock()

	vdb.embeddings[embedding.ID] = embedding
}

// Get retrieves an embedding by ID
func (vdb *VectorDB) Get(id string) (*CodeEmbedding, bool) {
	vdb.mu.RLock()
	defer vdb.mu.RUnlock()

	emb, exists := vdb.embeddings[id]
	return emb, exists
}

// Remove removes an embedding by ID
func (vdb *VectorDB) Remove(id string) {
	vdb.mu.Lock()
	defer vdb.mu.Unlock()

	delete(vdb.embeddings, id)
}

// GetAll returns all embeddings
func (vdb *VectorDB) GetAll() []*CodeEmbedding {
	vdb.mu.RLock()
	defer vdb.mu.RUnlock()

	embeddings := make([]*CodeEmbedding, 0, len(vdb.embeddings))
	for _, emb := range vdb.embeddings {
		embeddings = append(embeddings, emb)
	}

	return embeddings
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
func GenerateFileEmbedding(filePath string, fileInfo workspaceinfo.WorkspaceFileInfo) (*CodeEmbedding, error) {
	// For files, we'll use a combination of the file path, summary, and exports for the embedding
	// This gives us semantic information about the file's purpose and contents
	textForEmbedding := fmt.Sprintf("File: %s\nSummary: %s\nExports: %s", 
		filePath, fileInfo.Summary, fileInfo.Exports)
	
	vector, err := llm.GenerateEmbedding(textForEmbedding)
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

// GenerateWorkspaceEmbeddings generates embeddings for all files in a workspace
func GenerateWorkspaceEmbeddings(workspace workspaceinfo.WorkspaceFile, db *VectorDB) error {
	// Remove all existing file embeddings
	var toRemove []string
	for id := range db.embeddings {
		if emb, exists := db.Get(id); exists && emb.Type == "file" {
			toRemove = append(toRemove, id)
		}
	}
	
	for _, id := range toRemove {
		db.Remove(id)
	}

	// Generate new embeddings for all files
	for filePath, fileInfo := range workspace.Files {
		embedding, err := GenerateFileEmbedding(filePath, fileInfo)
		if err != nil {
			// Log error but continue with other files
			fmt.Printf("Warning: failed to generate embedding for file %s: %v\n", filePath, err)
			continue
		}
		db.Add(embedding)
	}

	return nil
}

// SearchRelevantFiles finds the most relevant files for a given query
func SearchRelevantFiles(query string, db *VectorDB, topK int) ([]*CodeEmbedding, []float64, error) {
	// Generate embedding for the query
	queryVector, err := llm.GenerateEmbedding(query)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate embedding for query: %w", err)
	}

	// Search for relevant file embeddings
	return db.Search(queryVector, topK)
}