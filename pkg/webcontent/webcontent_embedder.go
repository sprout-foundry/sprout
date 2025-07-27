package webcontent

import (
	"fmt"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/utils"
	"math"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"
)

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

	if llm.EstimateTokens(content) < 10000 {
		return content, nil // If content is small enough, return it directly
	}

	chunks := splitIntoChunks(content)
	if len(chunks) == 0 {
		return "", fmt.Errorf("failed to split content into chunks: %w", nil)
	}

	queryEmbedding, err := llm.GenerateEmbedding(query, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to generate embedding for query: %w", err)
	}

	textChunks := make([]*textChunk, len(chunks))
	var g errgroup.Group

	for i, chunk := range chunks {
		i, chunk := i, chunk // https://golang.org/doc/faq#closures_and_goroutines
		g.Go(func() error {
			embedding, err := llm.GenerateEmbedding(chunk, cfg)
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
