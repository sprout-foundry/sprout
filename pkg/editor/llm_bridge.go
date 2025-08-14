package editor

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/context"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
)

// getUpdatedCode bridges to the LLM and parses the response into file map.
func getUpdatedCode(originalCode, instructions, filename string, cfg *config.Config, imagePath string) (map[string]string, string, error) {
	log := utils.GetLogger(cfg.SkipPrompt)
	modelName, llmContent, err := context.GetLLMCodeResponse(cfg, originalCode, instructions, filename, imagePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get LLM response: %w", err)
	}

	log.Log(prompts.ModelReturned(modelName, llmContent))

	updatedCode, err := parser.GetUpdatedCodeFromResponse(llmContent)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse updated code from response: %w", err)
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
	return updatedCode, llmContent, nil
}
