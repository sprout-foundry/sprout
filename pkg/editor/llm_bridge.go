package editor

import (
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
	log.Log(fmt.Sprintf("Calling GetLLMCodeResponse with model: %s", cfg.EditingModel))

	modelName, llmContent, tokenUsage, err := context.GetLLMCodeResponse(cfg, originalCode, instructions, filename, imagePath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to get LLM response: %w", err)
	}

	log.Log(prompts.ModelReturned(modelName, llmContent))

	updatedCode := map[string]string{}
	var parseErr error

	// Prefer patch parsing first and materialize to full file contents
	if patches, perr := parser.GetUpdatedCodeFromPatchResponse(llmContent); perr == nil && len(patches) > 0 {
		for fname, p := range patches {
			content := parser.PatchToFullContent(p, originalCode)
			if strings.TrimSpace(content) != "" {
				updatedCode[fname] = content
			}
		}
		if len(updatedCode) == 0 {
			parseErr = fmt.Errorf("patches detected but no content materialized")
		}
	} else {
		parseErr = perr
		// Legacy extraction
		if uc, uerr := parser.GetUpdatedCodeFromResponse(llmContent); uerr == nil {
			updatedCode = uc
		} else {
			parseErr = uerr
		}
	}

	if len(updatedCode) == 0 {
		ui.Out().Print(prompts.NoCodeBlocksParsed() + "\n")
		ui.Out().Printf("%s\n", llmContent)
		// Fallback: if a filename was provided and the response contains a single code block
		// without filename headers, extract code by language and assign to that filename
		if strings.TrimSpace(filename) != "" {
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
