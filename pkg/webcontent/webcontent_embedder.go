package webcontent

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/embedding"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"

	"golang.org/x/sync/errgroup"
)

// LLMProvider defines the interface for an LLM provider
type LLMProvider interface {
	GetLLMResponse(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, imagePath ...string) (string, *types.TokenUsage, error)
}

// Embedder represents a web content embedder.
type Embedder struct {
	config   *config.Config
	logger   *utils.Logger
	provider LLMProvider
}

// NewEmbedder creates a new web content embedder.
func NewEmbedder(cfg *config.Config, logger *utils.Logger, provider LLMProvider) *Embedder {
	return &Embedder{
		config:   cfg,
		logger:   logger,
		provider: provider,
	}
}

const (
	chunkSize    = 1000 // characters
	chunkOverlap = 64   // characters
	topK         = 7    // number of top chunks to return
)

type textChunk struct {
	text      string
	embedding []float64
	score     float64
	index     int
}

// GetRelevantContentFromText uses embeddings to find the most relevant parts of a text for a given query.
func GetRelevantContentFromText(query, content string, cfg *config.Config) (string, error) {
	if query == "" || content == "" {
		return "", fmt.Errorf("query or content is empty, returning empty string: %w", nil)
	}

	if utils.EstimateTokens(content) < 10000 {
		return content, nil
	}

	chunks := splitIntoChunks(content)
	if len(chunks) == 0 {
		return "", fmt.Errorf("failed to split content into chunks: %w", nil)
	}

	queryEmbedding, err := embedding.GenerateEmbedding(query, cfg.EmbeddingModel)
	if err != nil {
		return "", fmt.Errorf("failed to generate embedding for query: %w", err)
	}

	textChunks := make([]*textChunk, len(chunks))
	var g errgroup.Group

	// Use semaphore to limit concurrent embedding requests to avoid rate limits
	maxConcurrent := cfg.MaxConcurrentRequests
	if maxConcurrent <= 0 {
		maxConcurrent = 3 // Default fallback
	}
	sem := make(chan struct{}, maxConcurrent)

	for i, chunk := range chunks {
		i, chunk := i, chunk // https://golang.org/doc/faq#closures_and_goroutines
		g.Go(func() error {
			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			embedding, err := embedding.GenerateEmbedding(chunk, cfg.EmbeddingModel)
			if err != nil {
				// Don't fail the whole process, just skip this chunk
				utils.GetLogger(cfg.SkipPrompt).Logf("failed to generate embedding for chunk: %v", err)
				return nil
			}
			textChunks[i] = &textChunk{
				text:      chunk,
				embedding: embedding,
				index:     i,
			}

			// Add small delay between requests to avoid rate limits
			if cfg.RequestDelayMs > 0 {
				time.Sleep(time.Duration(cfg.RequestDelayMs) * time.Millisecond)
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return "", err
	}

	validChunks := []*textChunk{}
	for _, tc := range textChunks {
		if tc != nil && len(tc.embedding) > 0 {
			tc.score = cosineSimilarity(queryEmbedding, tc.embedding)
			validChunks = append(validChunks, tc)
		}
	}

	sort.Slice(validChunks, func(i, j int) bool {
		return validChunks[i].score > validChunks[j].score
	})

	// Get top K chunks
	numChunks := len(validChunks)
	if numChunks > topK {
		numChunks = topK
	}
	topChunks := validChunks[:numChunks]

	// Sort top chunks by their original index to maintain some order
	sort.Slice(topChunks, func(i, j int) bool {
		return topChunks[i].index < topChunks[j].index
	})

	var relevantContentBuilder strings.Builder
	for _, tc := range topChunks {
		relevantContentBuilder.WriteString(tc.text)
		relevantContentBuilder.WriteString("\n\n...\n\n")
	}

	return strings.TrimSuffix(relevantContentBuilder.String(), "\n\n...\n\n"), nil
}

func splitIntoChunks(text string) []string {
	var chunks []string
	runes := []rune(text)
	if len(runes) == 0 {
		return chunks
	}

	for i := 0; i < len(runes); i += chunkSize - chunkOverlap {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
