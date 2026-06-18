//go:build !js

// browser_rod_result.go provides result-assembly helpers for the go-rod based
// BrowserRenderer: diagnostics capture, CORS detection, truncation utilities,
// and storage map extraction used by Run().

package webcontent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-rod/rod"
)

func captureStorageMap(page *rod.Page, script string) (map[string]string, error) {
	res, err := page.Eval(script)
	if err != nil {
		return nil, fmt.Errorf("capture storage map: %w", err)
	}
	if res == nil || res.Value.Nil() {
		return nil, nil
	}
	raw := []byte(res.Value.JSON("", ""))
	var parsed map[string]string
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode storage map: %w", err)
	}
	if len(parsed) == 0 {
		return nil, nil
	}
	return parsed, nil
}

func captureBrowserDiagnostics(page *rod.Page) ([]string, []string, []NetworkRequest, error) {
	res, err := page.Eval(`() => window.__sproutBrowserCapture || { console: [], errors: [], network: [] }`)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("capture browser diagnostics: %w", err)
	}
	payload := struct {
		Console []string         `json:"console"`
		Errors  []string         `json:"errors"`
		Network []NetworkRequest `json:"network"`
	}{}
	if err := json.Unmarshal([]byte(res.Value.JSON("", "")), &payload); err != nil {
		return nil, nil, nil, fmt.Errorf("decode browser diagnostics: %w", err)
	}
	return payload.Console, payload.Errors, payload.Network, nil
}

func detectCORSIssues(consoleMessages []string, pageErrors []string, networkRequests []NetworkRequest) []string {
	out := make([]string, 0)
	seen := make(map[string]struct{})
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}

	for _, value := range append(append([]string{}, consoleMessages...), pageErrors...) {
		lower := strings.ToLower(value)
		if strings.Contains(lower, "cors") ||
			strings.Contains(lower, "cross-origin") ||
			strings.Contains(lower, "same origin policy") ||
			strings.Contains(lower, "access-control-allow-origin") {
			add(value)
		}
	}
	for _, request := range networkRequests {
		lower := strings.ToLower(request.Error)
		if request.CORSBlocked ||
			strings.Contains(lower, "cors") ||
			strings.Contains(lower, "cross-origin") ||
			strings.Contains(lower, "access-control-allow-origin") {
			if request.URL != "" {
				add(fmt.Sprintf("CORS/network failure for %s %s: %s", request.Method, request.URL, strings.TrimSpace(request.Error)))
			} else {
				add(strings.TrimSpace(request.Error))
			}
		}
	}
	return out
}

func markCORSBlockedRequests(values []NetworkRequest) []NetworkRequest {
	out := make([]NetworkRequest, 0, len(values))
	for _, value := range values {
		combined := strings.ToLower(value.Error + " " + value.URL + " " + value.Initiator)
		if strings.Contains(combined, "cors") ||
			strings.Contains(combined, "cross-origin") ||
			strings.Contains(combined, "access-control-allow-origin") {
			value.CORSBlocked = true
		}
		out = append(out, value)
	}
	return out
}

func evalToJSONString(page *rod.Page, script string) (string, error) {
	res, err := page.Eval(script)
	if err != nil {
		return "", fmt.Errorf("failed to eval script: %w", err)
	}
	return res.Value.JSON("", "  "), nil
}

func truncateForBrowseResult(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(value[:limit]) + "... (truncated)"
}

func truncateStringSlice(values []string, maxItems int, itemLimit int) []string {
	if len(values) > maxItems {
		values = values[len(values)-maxItems:]
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, truncateForBrowseResult(value, itemLimit))
	}
	return out
}

func truncateNetworkRequests(values []NetworkRequest, maxItems int, itemLimit int) []NetworkRequest {
	if len(values) > maxItems {
		values = values[len(values)-maxItems:]
	}
	out := make([]NetworkRequest, 0, len(values))
	for _, value := range values {
		value.URL = truncateForBrowseResult(value.URL, itemLimit)
		value.Method = truncateForBrowseResult(value.Method, 64)
		value.Type = truncateForBrowseResult(value.Type, 64)
		value.Initiator = truncateForBrowseResult(value.Initiator, 64)
		value.Error = truncateForBrowseResult(value.Error, itemLimit)
		out = append(out, value)
	}
	return out
}

func textLimit(responseMaxChars int) int {
	if responseMaxChars > 0 {
		return responseMaxChars
	}
	return 4000
}

func domLimit(responseMaxChars int) int {
	if responseMaxChars > 0 {
		return responseMaxChars
	}
	return 12000
}
