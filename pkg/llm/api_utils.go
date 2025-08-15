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

	"github.com/alantheprice/ledit/pkg/prompts"
)

// removeThinkTags removes  blocks from the content.
func removeThinkTags(content string) string {
	re := regexp.MustCompile(`(?s)`)
	return re.ReplaceAllString(content, "")
}

// IsGeminiModel checks if the given model name is a Gemini model.
func IsGeminiModel(modelName string) bool {
	lowerModelName := strings.ToLower(modelName)
	return strings.Contains(lowerModelName, "gemini")
}

// IsOllamaModel checks if the given model name is an Ollama model.
func IsOllamaModel(modelName string) bool {
	return strings.HasPrefix(strings.ToLower(modelName), "ollama:")
}

// ShouldUseJSONResponse inspects the messages to determine if the prompt
// explicitly requires strict JSON output. When true, callers may enable
// provider JSON mode (e.g., DeepInfra/OpenAI response_format {type: "json_object"}).
func ShouldUseJSONResponse(messages []prompts.Message) bool {
	// Scan both system and user messages for explicit JSON-only directives
	var haystack []string
	for _, m := range messages {
		switch v := m.Content.(type) {
		case string:
			haystack = append(haystack, v)
		case []prompts.ContentPart:
			// Collect text parts
			for _, p := range v {
				if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
					haystack = append(haystack, p.Text)
				}
			}
		}
	}
	if len(haystack) == 0 {
		return false
	}

	indicators := []string{
		"Respond only with valid JSON",
		"Always respond with valid JSON",
		"Respond with STRICT JSON",
		"Your response MUST be a JSON object",
		"Your response MUST be a JSON array",
		"Respond with ONLY JSON",
		"Return JSON only",
		"Output should be a JSON array",
		"Output should be JSON",
	}
	blob := strings.ToLower(strings.Join(haystack, "\n"))
	for _, s := range indicators {
		if strings.Contains(blob, strings.ToLower(s)) {
			return true
		}
	}
	return false
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
	resp, err := client.Do(req)
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
