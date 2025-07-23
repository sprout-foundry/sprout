package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math" // Import math for dot product and magnitude
	"net/http"

	"ledit/pkg/config"
)

type EmbeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type EmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

// GenerateEmbedding generates an embedding for the given input using Ollama.
func GenerateEmbedding(input string, cfg *config.Config) ([]float64, error) {
	embeddingModel := cfg.EmbeddingModel
	if embeddingModel == "" {
		// A sensible default from https://ollama.com/blog/embedding-models
		embeddingModel = "mxbai-embed-large"
	}

	reqData := EmbeddingRequest{
		Model: embeddingModel,
		Input: input,
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	// Use cfg.OllamaServerURL for the Ollama API endpoint
	resp, err := http.Post(cfg.OllamaServerURL+"/api/embed", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to call ollama embedding api: %w. Make sure ollama is running and the model '%s' is pulled", err, embeddingModel)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embedding api returned non-200 status: %s, body: %s", resp.Status, string(body))
	}

	var embResp EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("failed to decode ollama embedding response: %w", err)
	}

	return embResp.Embedding, nil
}

// CosineSimilarity calculates the cosine similarity between two vectors.
func CosineSimilarity(vec1, vec2 []float64) (float64, error) {
	if len(vec1) != len(vec2) {
		return 0.0, fmt.Errorf("vectors must have the same dimension")
	}

	dotProduct := 0.0
	magnitude1 := 0.0
	magnitude2 := 0.0

	for i := 0; i < len(vec1); i++ {
		dotProduct += vec1[i] * vec2[i]
		magnitude1 += vec1[i] * vec1[i]
		magnitude2 += vec2[i] * vec2[i]
	}

	magnitude1 = math.Sqrt(magnitude1)
	magnitude2 = math.Sqrt(magnitude2)

	if magnitude1 == 0 || magnitude2 == 0 {
		return 0.0, fmt.Errorf("one or both vectors have zero magnitude")
	}

	return dotProduct / (magnitude1 * magnitude2), nil
}
