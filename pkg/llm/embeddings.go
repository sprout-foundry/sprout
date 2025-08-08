package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math" // Import math for dot product and magnitude
	"net/http"
	"os" // Import os for environment variable check
	"strings"

	"github.com/alantheprice/ledit/pkg/apikeys" // Changed import from pkg/config to pkg/apikeys
)

const jinaEmbeddingsURL = "https://api.jina.ai/v1/embeddings"
const deepInfraEmbeddingsURL = "https://api.deepinfra.com/v1/openai/embeddings"

// JinaEmbeddingRequest represents the request body for the Jina AI Embeddings API.
type JinaEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"` // Jina expects an array of strings
}

// JinaEmbeddingResponse represents the response body from the Jina AI Embeddings API.
type JinaEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// OpenAIEmbeddingRequest represents the request body for OpenAI-compatible Embeddings API.
type OpenAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// OpenAIEmbeddingResponse represents the response body from OpenAI-compatible Embeddings API.
type OpenAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// generateJinaEmbedding generates an embedding for the given input using Jina AI.
func generateJinaEmbedding(input string) ([]float64, error) {
	apiKey, err := apikeys.GetAPIKey("JinaAI", false)
	if err != nil || apiKey == "" {
		apiKey = os.Getenv("JINA_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("Jina AI API key not found. Please set JINA_API_KEY environment variable or provide it when prompted.")
		}
	}

	embeddingModel := "jina-embeddings-v4"

	reqData := JinaEmbeddingRequest{
		Model: embeddingModel,
		Input: []string{input},
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Jina embedding request: %w", err)
	}

	req, err := http.NewRequest("POST", jinaEmbeddingsURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create Jina embedding request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Jina embedding API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Jina embedding API returned non-200 status: %s, body: %s", resp.Status, string(body))
	}

	var jinaResp JinaEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&jinaResp); err != nil {
		return nil, fmt.Errorf("failed to decode Jina embedding response: %w", err)
	}

	if len(jinaResp.Data) == 0 || len(jinaResp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("Jina embedding response did not contain expected embedding data")
	}

	return jinaResp.Data[0].Embedding, nil
}

// generateDeepInfraEmbedding generates an embedding for the given input using DeepInfra.
func generateDeepInfraEmbedding(input string) ([]float64, error) {
	apiKey, err := apikeys.GetAPIKey("deepinfra", false)
	if err != nil || apiKey == "" {
		apiKey = os.Getenv("DEEPINFRA_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("DeepInfra API key not found. Please set DEEPINFRA_API_KEY environment variable or provide it when prompted.")
		}
	}

	embeddingModel := "Qwen/Qwen3-Embedding-4B"

	reqData := OpenAIEmbeddingRequest{
		Model: embeddingModel,
		Input: []string{input},
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DeepInfra embedding request: %w", err)
	}

	req, err := http.NewRequest("POST", deepInfraEmbeddingsURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create DeepInfra embedding request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call DeepInfra embedding API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DeepInfra embedding API returned non-200 status: %s, body: %s", resp.Status, string(body))
	}

	var deepInfraResp OpenAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&deepInfraResp); err != nil {
		return nil, fmt.Errorf("failed to decode DeepInfra embedding response: %w", err)
	}

	if len(deepInfraResp.Data) == 0 || len(deepInfraResp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("DeepInfra embedding response did not contain expected embedding data")
	}

	return deepInfraResp.Data[0].Embedding, nil
}

// GenerateEmbedding generates an embedding for the given input using the specified model.
func GenerateEmbedding(input, modelName string) ([]float64, error) {
	parts := strings.SplitN(modelName, ":", 2)
	provider := parts[0]

	switch provider {
	case "jina":
		return generateJinaEmbedding(input)
	case "deepinfra":
		return generateDeepInfraEmbedding(input)
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", provider)
	}
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
