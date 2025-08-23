package llm

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
)

// retryWithBackoffUtils executes an HTTP request with a single retry on 500 errors
// with a 200ms backoff delay
func retryWithBackoffUtils(req *http.Request, client *http.Client) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return resp, err
	}

	// If we get a 500 error, wait 200ms and retry once
	if resp.StatusCode == 500 {
		// Close the first response body
		resp.Body.Close()

		// Create a new request for retry (GET requests don't have a body)
		time.Sleep(200 * time.Millisecond)

		// Retry the request
		resp, err = client.Do(req)
	}

	return resp, err
}

// removeThinkTags removes  blocks from the content.
func removeThinkTags(content string) string {
	re := regexp.MustCompile(`(?s)`)
	return re.ReplaceAllString(content, "")
}

// EncodeImageToBase64 reads an image file and encodes it as base64
func EncodeImageToBase64(imagePath string) (string, error) {
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("failed to read image file: %w", err)
	}

	// Get the file extension to determine MIME type
	ext := strings.ToLower(filepath.Ext(imagePath))
	var mimeType string
	switch ext {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".png":
		mimeType = "image/png"
	case ".gif":
		mimeType = "image/gif"
	case ".webp":
		mimeType = "image/webp"
	default:
		mimeType = "image/jpeg" // Default fallback
	}

	base64String := base64.StdEncoding.EncodeToString(imageData)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64String), nil
}

// GetMessageText extracts text content from a message, handling both string and multimodal content
func GetMessageText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []prompts.ContentPart:
		var textParts []string
		for _, part := range v {
			if part.Type == "text" {
				textParts = append(textParts, part.Text)
			}
		}
		return strings.Join(textParts, " ")
	default:
		return ""
	}
}

// IsMultimodalContent checks if the message content contains images
func IsMultimodalContent(content interface{}) bool {
	if parts, ok := content.([]prompts.ContentPart); ok {
		for _, part := range parts {
			if part.Type == "image_url" {
				return true
			}
		}
	}
	return false
}
func AddImageToMessage(message *prompts.Message, imagePath string) error {
	if imagePath == "" {
		return nil // Nothing to add
	}

	imageDataURL, err := EncodeImageToBase64(imagePath)
	if err != nil {
		return fmt.Errorf("failed to encode image: %w", err)
	}

	// Convert message content to multimodal format
	var parts []prompts.ContentPart

	// Add existing text content
	if contentStr, ok := message.Content.(string); ok && contentStr != "" {
		parts = append(parts, prompts.ContentPart{
			Type: "text",
			Text: contentStr,
		})
	}

	// Add image content
	parts = append(parts, prompts.ContentPart{
		Type: "image_url",
		ImageURL: &prompts.ImageURL{
			URL:    imageDataURL,
			Detail: "high",
		},
	})

	message.Content = parts
	return nil
}

// CheckEndpointReachable performs a quick GET to verify the endpoint is reachable
func CheckEndpointReachable(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := retryWithBackoffUtils(req, client)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// RouteModels selects control and editing models based on task category and approximate size.
// category: "docs" | "code" | "test" | "review" (others map to "code").
// approxSize: approximate content size in characters or bytes.
// RouteModels has been removed. Control/edit model selection is done directly at call sites.

// GetSmartTimeout returns an appropriate timeout for the given model and operation type
// If no specific timeout is configured, it returns a sensible default based on the model characteristics
func GetSmartTimeout(cfg *config.Config, modelName string, operationType string) time.Duration {
	if cfg != nil {
		return cfg.GetSmartTimeout(modelName, operationType)
	}

	// Fallback logic if config is not available
	baseTimeout := 120 * time.Second // 2 minutes default

	// Adjust for known slow providers/models
	if strings.Contains(modelName, "deepinfra") {
		baseTimeout = 180 * time.Second // 3 minutes for DeepInfra
	} else if strings.Contains(modelName, "ollama") {
		baseTimeout = 300 * time.Second // 5 minutes for local models
	} else if strings.Contains(modelName, "deepseek-r1") || strings.Contains(modelName, "DeepSeek-R1") {
		baseTimeout = 300 * time.Second // 5 minutes for reasoning models
	}

	// Adjust for operation type
	switch operationType {
	case "code_review", "analysis":
		return baseTimeout + (30 * time.Second)
	case "search", "quick":
		return time.Duration(float64(baseTimeout) * 0.5)
	case "commit", "summary":
		return time.Duration(float64(baseTimeout) * 0.75)
	default:
		return baseTimeout
	}
}

// GetDefaultTimeout returns a sensible default timeout for LLM operations
// This is used as a fallback when no specific timeout is configured
func GetDefaultTimeout(modelName string) time.Duration {
	// Use a conservative default that should work for most cases
	return GetSmartTimeout(nil, modelName, "default")
}
