// Tool result constraint: truncation and compaction of tool results
// before they are sent to the model context window.
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

func constrainToolResultForModel(toolName string, args map[string]interface{}, result string) string {
	if toolName == "analyze_image_content" {
		return compactAnalyzeImageResultForModel(result)
	}

	if toolName != "fetch_url" {
		return result
	}

	maxChars := defaultFetchURLResultMaxChars
	if raw := strings.TrimSpace(os.Getenv("LEDIT_FETCH_URL_MAX_CHARS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxChars = parsed
		}
	}

	if len(result) <= maxChars {
		return result
	}

	headLen := maxChars * 70 / 100
	tailLen := maxChars - headLen
	if tailLen <= 0 {
		tailLen = maxChars / 2
		headLen = maxChars - tailLen
	}

	omitted := len(result) - (headLen + tailLen)
	if omitted < 0 {
		omitted = 0
	}

	archivePath, archiveErr := saveFetchURLOutputToFile(args, result)
	notice := buildFetchURLTruncationNotice(omitted, archivePath, archiveErr)
	return result[:headLen] + notice + result[len(result)-tailLen:]
}

func buildFetchURLTruncationNotice(omitted int, archivePath string, archiveErr error) string {
	if archivePath == "" {
		if archiveErr != nil {
			return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_FETCH_URL_MAX_CHARS to adjust. Failed to save full output: %v]\n\n", omitted, archiveErr)
		}
		return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_FETCH_URL_MAX_CHARS to adjust. Full output path unavailable.]\n\n", omitted)
	}
	return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_FETCH_URL_MAX_CHARS to adjust. Full output saved to %s]\n\n", omitted, archivePath)
}

func saveFetchURLOutputToFile(args map[string]interface{}, output string) (string, error) {
	dir := strings.TrimSpace(os.Getenv("LEDIT_FETCH_URL_ARCHIVE_DIR"))
	if dir == "" {
		dir = defaultFetchURLArchiveDir
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create archive directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("fetch_url_%s_%d.txt", timestamp, time.Now().UnixNano()%1_000_000)
	path := filepath.Join(dir, filename)

	header := ""
	if args != nil {
		if rawURL, ok := args["url"].(string); ok && strings.TrimSpace(rawURL) != "" {
			header = fmt.Sprintf("URL: %s\nFetched-At: %s\n\n", strings.TrimSpace(rawURL), time.Now().Format(time.RFC3339))
		}
	}

	fullOutput := output
	if header != "" {
		fullOutput = header + output
	}

	if err := os.WriteFile(path, []byte(fullOutput), 0o644); err != nil {
		return "", fmt.Errorf("failed to write fetch URL output file: %w", err)
	}
	return path, nil
}

func compactAnalyzeImageResultForModel(result string) string {
	var parsed tools.ImageAnalysisResponse
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		return result
	}

	var b strings.Builder
	b.WriteString("analyze_image_content result:\n")
	b.WriteString(fmt.Sprintf("- success: %t\n", parsed.Success))
	if parsed.InputPath != "" {
		b.WriteString(fmt.Sprintf("- input_path: %s\n", parsed.InputPath))
	}
	if parsed.InputType != "" {
		b.WriteString(fmt.Sprintf("- input_type: %s\n", parsed.InputType))
	}
	b.WriteString(fmt.Sprintf("- ocr_attempted: %t\n", parsed.OCRAttempted))

	if parsed.ErrorCode != "" {
		b.WriteString(fmt.Sprintf("- error_code: %s\n", parsed.ErrorCode))
	}
	if parsed.ErrorMessage != "" {
		b.WriteString(fmt.Sprintf("- error_message: %s\n", parsed.ErrorMessage))
	}

	excerpt := strings.TrimSpace(parsed.ExtractedText)
	if excerpt == "" && parsed.Analysis != nil {
		excerpt = strings.TrimSpace(parsed.Analysis.Description)
	}
	if excerpt != "" {
		originalChars := parsed.OriginalChars
		if originalChars == 0 {
			originalChars = len(excerpt)
		}
		returnedChars := parsed.ReturnedChars
		if returnedChars == 0 {
			returnedChars = len(excerpt)
		}
		b.WriteString(fmt.Sprintf("- extracted_chars: %d\n", originalChars))
		b.WriteString(fmt.Sprintf("- returned_chars: %d\n", returnedChars))
	}
	if parsed.OutputTruncated {
		b.WriteString("- tool_output_truncated: true\n")
	}
	if parsed.FullOutputPath != "" {
		b.WriteString(fmt.Sprintf("- full_output_path: %s\n", parsed.FullOutputPath))
	}
	if parsed.Analysis != nil {
		if len(parsed.Analysis.Elements) > 0 {
			b.WriteString(fmt.Sprintf("- detected_elements: %d\n", len(parsed.Analysis.Elements)))
		}
		if len(parsed.Analysis.Issues) > 0 {
			b.WriteString(fmt.Sprintf("- issues: %s\n", strings.Join(parsed.Analysis.Issues, "; ")))
		}
		if len(parsed.Analysis.Suggestions) > 0 {
			b.WriteString(fmt.Sprintf("- suggestions: %s\n", strings.Join(parsed.Analysis.Suggestions, "; ")))
		}
	}
	if excerpt != "" {
		b.WriteString("- extracted_text_excerpt:\n")
		b.WriteString(limitAnalyzeImageExcerpt(excerpt, defaultAnalyzeImageResultExcerptChars))
	}

	return strings.TrimSpace(b.String())
}

func limitAnalyzeImageExcerpt(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}

	suffix := fmt.Sprintf("\n[EXCERPT TRUNCATED: kept first %d of %d chars]", maxChars, len(text))
	keep := maxChars - len(suffix)
	if keep < 0 {
		keep = maxChars
		suffix = ""
	}
	return strings.TrimSpace(text[:keep]) + suffix
}
