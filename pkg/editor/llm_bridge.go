package editor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/context"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
)

// getUpdatedCode bridges to the LLM and parses the response into file map.
func getUpdatedCode(originalCode, instructions, filename string, cfg *config.Config, imagePath string) (map[string]string, string, *llm.TokenUsage, error) {
	log := utils.GetLogger(cfg.SkipPrompt)
	log.Log("=== getUpdatedCode Debug ===")
	log.Log(fmt.Sprintf("Instructions: %s", instructions))
	log.Log(fmt.Sprintf("Calling GetLLMCodeResponse with model: %s", cfg.EditingModel))

	modelName, llmContent, tokenUsage, err := context.GetLLMCodeResponse(cfg, originalCode, instructions, filename, imagePath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to get LLM response: %w", err)
	}

	log.Log(prompts.ModelReturned(modelName, llmContent))
	// DEBUG: Print the actual response content to see what's being returned
	maxLen := 200
	if len(llmContent) < maxLen {
		maxLen = len(llmContent)
	}
	log.Log(fmt.Sprintf("DEBUG: Raw LLM Content (first %d chars): '%s'", maxLen, llmContent[:maxLen]))

	updatedCode := map[string]string{}
	var parseErr error

	// Prefer patch parsing first and materialize to full file contents
	if patches, perr := parser.GetUpdatedCodeFromPatchResponse(llmContent); perr == nil && len(patches) > 0 {
		for fname, p := range patches {
			content := parser.PatchToFullContent(p, fname)
			if strings.TrimSpace(content) != "" {
				updatedCode[fname] = content
			}
		}
		if len(updatedCode) == 0 {
			parseErr = fmt.Errorf("patches detected but no content materialized")
		}
	} else {
		parseErr = perr
		// Try JSON parsing first
		if jsonCode, jsonErr := parseJSONResponse(llmContent); jsonErr == nil && len(jsonCode) > 0 {
			updatedCode = jsonCode
			log.Log("Successfully parsed JSON response")
		} else {
			// Legacy extraction fallback
			if uc, uerr := parser.GetUpdatedCodeFromResponse(llmContent); uerr == nil {
				updatedCode = uc
				log.Log("Successfully parsed legacy response")
			} else {
				parseErr = uerr
				log.Log(fmt.Sprintf("All parsing methods failed: patch=%v, json=%v, legacy=%v", perr, jsonErr, uerr))
			}
		}
	}

	if len(updatedCode) == 0 {
		ui.Out().Print(prompts.NoCodeBlocksParsed() + "\n")
		ui.Out().Printf("%s\n", llmContent)
		// Fallback: if a filename was provided and the response contains a single code block
		// without filename headers, extract code by language and assign to that filename
		if jsonCode, jsonErr := parseJSONResponse(llmContent); jsonErr == nil && len(jsonCode) > 0 {
			updatedCode = jsonCode
			log.Log("Successfully parsed JSON response in fallback")
		} else if strings.TrimSpace(filename) != "" {
			lang := getLanguageFromExtension(filename)
			if codeOnly, perr := parser.ExtractCodeFromResponse(llmContent, lang); perr == nil && strings.TrimSpace(codeOnly) != "" {
				updatedCode = map[string]string{filename: codeOnly}
			} else if anyCode, perr2 := parser.ExtractCodeFromResponse(llmContent, ""); perr2 == nil && strings.TrimSpace(anyCode) != "" {
				updatedCode = map[string]string{filename: anyCode}
			}
		}
	}

	if len(updatedCode) == 0 && parseErr != nil {
		return nil, "", tokenUsage, fmt.Errorf("failed to parse model response in patch mode: %w", parseErr)
	}

	return updatedCode, llmContent, tokenUsage, nil
}

// parseJSONResponse parses JSON-formatted code responses
func parseJSONResponse(llmContent string) (map[string]string, error) {
	// Try to parse as JSON
	var jsonResponse struct {
		FilePath    string `json:"file_path"`
		FileContent string `json:"file_content"`
	}

	// Clean up the response - sometimes LLMs add extra text
	content := strings.TrimSpace(llmContent)

	// Try to extract JSON from the response
	if err := json.Unmarshal([]byte(content), &jsonResponse); err != nil {
		// Try to find JSON within the response
		start := strings.Index(content, "{")
		end := strings.LastIndex(content, "}")
		if start >= 0 && end > start {
			jsonPart := content[start : end+1]
			if err := json.Unmarshal([]byte(jsonPart), &jsonResponse); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	if jsonResponse.FilePath == "" || jsonResponse.FileContent == "" {
		return nil, fmt.Errorf("invalid JSON response: missing file_path or file_content")
	}

	result := make(map[string]string)
	result[jsonResponse.FilePath] = jsonResponse.FileContent
	return result, nil
}
