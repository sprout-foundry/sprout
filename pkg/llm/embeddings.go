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

	"github.com/alantheprice/ledit/pkg/apikeys"
)

const deepInfraEmbeddingsURL = "https://api.deepinfra.com/v1/openai/embeddings"

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

// generateDeepInfraEmbedding generates an embedding for the given input using DeepInfra.
func generateDeepInfraEmbedding(input string, model string) ([]float64, error) {
	apiKey, err := apikeys.GetAPIKey("deepinfra", false)
	if err != nil || apiKey == "" {
		apiKey = os.Getenv("DEEPINFRA_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("DeepInfra API key not found. Please set DEEPINFRA_API_KEY environment variable or provide it when prompted.")
		}
	}

	// Use the provided model, or default if empty
	embeddingModel := model
	if embeddingModel == "" {
		embeddingModel = "Qwen/Qwen3-Embedding-4B"
	}

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

	// Read the entire response body first to allow for logging on JSON decode failure
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DeepInfra embedding response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Use the already read bodyBytes for the error message
		return nil, fmt.Errorf("DeepInfra embedding API returned non-200 status: %s, body: %s", resp.Status, string(bodyBytes))
	}

	var deepInfraResp OpenAIEmbeddingResponse
	// Decode from the read bodyBytes
	if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&deepInfraResp); err != nil {
		// Include the raw body in the error message for debugging JSON parsing issues
		return nil, fmt.Errorf("failed to decode DeepInfra embedding response: %w, raw body: %s", err, string(bodyBytes))
	}

	if len(deepInfraResp.Data) == 0 || len(deepInfraResp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("DeepInfra embedding response did not contain expected embedding data, raw body: %s", string(bodyBytes))
	}

	return deepInfraResp.Data[0].Embedding, nil
}

// GenerateEmbedding generates an embedding for the given input using the specified model.
// It currently only supports DeepInfra embeddings.
func GenerateEmbedding(input, modelName string) ([]float64, error) {
	provider := "deepinfra" // Default provider
	model := ""

	if modelName != "" {
		parts := strings.SplitN(modelName, ":", 2)
		if len(parts) > 0 && parts[0] != "" {
			provider = parts[0]
		}
		if len(parts) > 1 {
			model = parts[1]
		}
	}

	switch provider {
	case "deepinfra":
		return generateDeepInfraEmbedding(input, model)
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s. Only 'deepinfra' is currently supported.", provider)
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