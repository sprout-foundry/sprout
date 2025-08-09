package llm

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
