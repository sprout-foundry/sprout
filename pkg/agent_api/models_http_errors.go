package api

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
)

const maxHTTPErrorBodyPreview = 240

// FormatHTTPResponseError converts an HTTP error response into a concise,
// user-facing error that avoids dumping full HTML or JSON payloads.
func FormatHTTPResponseError(statusCode int, headers http.Header, body []byte) error {
	message := summarizeHTTPResponseError(statusCode, headers, body)
	if message == "" {
		return fmt.Errorf("HTTP %d", statusCode)
	}
	return fmt.Errorf("HTTP %d: %s", statusCode, message)
}

func summarizeHTTPResponseError(statusCode int, headers http.Header, body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	if jsonMsg := extractHTTPJSONErrorMessage(body); jsonMsg != "" {
		return limitHTTPErrorText(jsonMsg)
	}

	if looksLikeHTMLErrorPage(headers, trimmed) {
		return summarizeHTMLErrorPage(statusCode, trimmed)
	}

	return limitHTTPErrorText(trimmed)
}

func extractHTTPJSONErrorMessage(body []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(extractHTTPJSONErrorField(payload))
}

func extractHTTPJSONErrorField(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]interface{}:
		for _, key := range []string{"error", "message", "detail", "details", "title", "reason"} {
			if msg := extractHTTPJSONErrorField(typed[key]); msg != "" {
				return msg
			}
		}
	case []interface{}:
		for _, item := range typed {
			if msg := extractHTTPJSONErrorField(item); msg != "" {
				return msg
			}
		}
	}
	return ""
}

func looksLikeHTMLErrorPage(headers http.Header, body string) bool {
	contentType := strings.ToLower(headers.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml") {
		return true
	}

	lowerBody := strings.ToLower(strings.TrimSpace(body))
	return strings.HasPrefix(lowerBody, "<!doctype html") ||
		strings.HasPrefix(lowerBody, "<html") ||
		strings.Contains(lowerBody, "<title>")
}

func summarizeHTMLErrorPage(statusCode int, body string) string {
	lowerBody := strings.ToLower(body)
	if strings.Contains(lowerBody, "cloudflare") {
		switch {
		case statusCode == 524 || strings.Contains(lowerBody, "error code 524"):
			return "upstream timeout (Cloudflare 524 HTML error page)"
		case statusCode >= 520 && statusCode <= 527:
			return fmt.Sprintf("gateway error (Cloudflare %d HTML error page)", statusCode)
		default:
			return "gateway error (Cloudflare HTML error page)"
		}
	}

	if title := extractHTMLTitle(body); title != "" {
		return fmt.Sprintf("%s (HTML error page)", limitHTTPErrorText(title))
	}

	if statusCode == http.StatusGatewayTimeout {
		return "upstream timeout (HTML error page)"
	}

	return "received HTML error page"
}

func extractHTMLTitle(body string) string {
	lowerBody := strings.ToLower(body)
	start := strings.Index(lowerBody, "<title>")
	if start == -1 {
		return ""
	}
	start += len("<title>")
	end := strings.Index(lowerBody[start:], "</title>")
	if end == -1 {
		return ""
	}
	title := html.UnescapeString(body[start : start+end])
	return strings.TrimSpace(strings.Join(strings.Fields(title), " "))
}

func limitHTTPErrorText(text string) string {
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if text == "" {
		return ""
	}
	if len(text) <= maxHTTPErrorBodyPreview {
		return text
	}
	return text[:maxHTTPErrorBodyPreview-3] + "..."
}
