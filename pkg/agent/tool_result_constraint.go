// Tool result constraint: truncation and compaction of tool results
// before they are sent to the model context window. It also owns the shared
// result-size limits and universal truncation helper moved from the legacy
// tool executor configuration.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

const maxToolFailureMessageChars = 4000     // ~1000 tokens worst-case (4 chars/token heuristic)
const defaultFetchURLResultMaxChars = 80000 // Raised from 60000 to 80000 (better web content coverage)
// defaultFetchURLArchiveDir returns the directory where over-limit fetch_url
// output is archived. Rooted at os.TempDir() so it is writable on every
// supported platform — including Termux, where /tmp is not writable but
// $TMPDIR (the value os.TempDir() resolves to) is. On plain Linux this
// still yields /tmp/sprout/downloads.
func defaultFetchURLArchiveDir() string {
	return filepath.Join(os.TempDir(), "sprout", "downloads")
}

const defaultToolResultMaxChars = 50000 // Universal cap on tool result size (~12K tokens)

// truncateToolResult truncates large tool results to prevent blowing up the LLM context window.
// Keeps the first 45K chars and last 5K chars with a truncation notice in between.
func truncateToolResult(result string) string {
	if len(result) <= defaultToolResultMaxChars {
		return result
	}

	headChars := 45000
	tailChars := 5000
	omitted := len(result) - headChars - tailChars

	packageLogWarnf("tool result truncated: %d -> %d chars (omitted %d)", len(result), defaultToolResultMaxChars, omitted)

	return result[:headChars] + fmt.Sprintf("\n[... truncated: %d chars omitted. Total was %d chars ...]\n", omitted, len(result)) + result[len(result)-tailChars:]
}

func constrainToolResultForModel(toolName string, args map[string]interface{}, result string) string {

	if toolName != "fetch_url" {
		return result
	}

	maxChars := defaultFetchURLResultMaxChars
	if raw := strings.TrimSpace(envutil.GetEnvSimple("FETCH_URL_MAX_CHARS")); raw != "" {
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
			return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set SPROUT_FETCH_URL_MAX_CHARS to adjust. Failed to save full output: %v]\n\n", omitted, archiveErr)
		}
		return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set SPROUT_FETCH_URL_MAX_CHARS to adjust. Full output path unavailable.]\n\n", omitted)
	}
	return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set SPROUT_FETCH_URL_MAX_CHARS to adjust. Full output saved to %s]\n\n", omitted, archivePath)
}

func saveFetchURLOutputToFile(args map[string]interface{}, output string) (string, error) {
	dir := strings.TrimSpace(envutil.GetEnvSimple("FETCH_URL_ARCHIVE_DIR"))
	if dir == "" {
		dir = defaultFetchURLArchiveDir()
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", agenterrors.Wrap(err, "failed to create archive directory")
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
		return "", agenterrors.Wrap(err, "failed to write fetch URL output file")
	}
	return path, nil
}
