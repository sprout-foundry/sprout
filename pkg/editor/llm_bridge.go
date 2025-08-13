package editor

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/context"
	"github.com/alantheprice/ledit/pkg/parser"
	"github.com/alantheprice/ledit/pkg/prompts"
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
		fmt.Println(prompts.NoCodeBlocksParsed())
		fmt.Printf("%s\n", llmContent)
	}
	return updatedCode, llmContent, nil
}
