package tools

import (
	"context"
	"mime"
	"net/url"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// fetchURLHandler implements ToolHandler for the fetch_url tool.
type fetchURLHandler struct{}

func (h *fetchURLHandler) Name() string {
	return "fetch_url"
}

func (h *fetchURLHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "fetch_url",
		Description: "Fetch and extract content from a URL. For HTML/text content, extracts readable text. For images and PDFs (when the model supports vision), returns visual content directly.",
		Parameters: []ParameterDef{
			{
				Name:        "url",
				Type:        "string",
				Required:    true,
				Description: "URL to fetch content from.",
			},
		},
		Required: []string{"url"},
	}
}

func (h *fetchURLHandler) Validate(args map[string]any) error {
	raw, err := extractString(args, "url")
	if err != nil {
		return err
	}

	u := strings.TrimSpace(raw)
	if u == "" {
		return agenterrors.NewValidation("parameter 'url' must not be empty", nil)
	}

	// Must be an absolute HTTP(S) URL.
	parsed, perr := url.ParseRequestURI(u)
	if perr != nil {
		return agenterrors.NewValidation("parameter 'url' must be an absolute HTTP(S) URL", nil)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return agenterrors.NewValidation("parameter 'url' must be an absolute HTTP(S) URL", nil)
	}

	return nil
}

func (h *fetchURLHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	urlVal, err := extractString(args, "url")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	content, err := FetchURL(urlVal, env.ConfigManager)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, err
	}

	result := ToolResult{
		Output:     content,
		TokenUsage: int64(estimateTokenUsage(content)),
	}

	// Detect image / PDF URLs and attach an ImageData entry so vision-capable
	// models can render the resource directly.
	if imageData := classifyURL(urlVal); imageData != nil {
		result.Images = []ImageData{*imageData}
	}

	return result, nil
}

func (h *fetchURLHandler) Aliases() []string         { return nil }
func (h *fetchURLHandler) Timeout() time.Duration    { return 0 }
func (h *fetchURLHandler) MaxResultSize() int        { return 0 }
func (h *fetchURLHandler) SafeForParallel() bool     { return false }
func (h *fetchURLHandler) Interactive() bool         { return false }
// so, returns a populated ImageData.  Returns nil for non-media URLs.
func classifyURL(rawURL string) *ImageData {
	_, path := splitURLScheme(rawURL)
	ext := strings.ToLower(fileURLExtension(path))

	switch ext {
	case ".png":
		return &ImageData{URI: rawURL, MIMEType: "image/png"}
	case ".jpg", ".jpeg":
		return &ImageData{URI: rawURL, MIMEType: "image/jpeg"}
	case ".gif":
		return &ImageData{URI: rawURL, MIMEType: "image/gif"}
	case ".webp":
		return &ImageData{URI: rawURL, MIMEType: "image/webp"}
	case ".pdf":
		return &ImageData{URI: rawURL, MIMEType: "application/pdf"}
	}

	// Fallback: use the mime package for uncommon extensions.
	if mime := mime.TypeByExtension(ext); mime != "" && strings.HasPrefix(mime, "image/") {
		return &ImageData{URI: rawURL, MIMEType: mime}
	}

	return nil
}

// splitURLScheme returns the scheme and the remainder of the URL (after
// scheme://).  Handles both absolute and relative paths gracefully.
func splitURLScheme(rawURL string) (string, string) {
	if u, err := url.Parse(rawURL); err == nil {
		return u.Scheme, u.Path
	}
	// Best-effort fallback for malformed URLs.
	idx := strings.Index(rawURL, "://")
	if idx == -1 {
		return "", rawURL
	}
	return rawURL[:idx], rawURL[idx+3:]
}

// fileURLExtension returns the file extension from a URL path.
// Returns an empty string if there is no extension.
func fileURLExtension(path string) string {
	idx := strings.LastIndex(path, ".")
	if idx == -1 {
		return ""
	}
	return path[idx:]
}
