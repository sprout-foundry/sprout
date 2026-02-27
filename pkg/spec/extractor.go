package spec

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

// SpecExtractor extracts canonical specs from conversations
type SpecExtractor struct {
	agentClient api.ClientInterface
	logger      *utils.Logger
	cfg         *configuration.Config
}

// NewSpecExtractor creates a new spec extractor
func NewSpecExtractor(cfg *configuration.Config, logger *utils.Logger) (*SpecExtractor, error) {
	agentClient, err := resolveSpecAgentClient(cfg, logger, "Spec extraction")
	if err != nil {
		return nil, err
	}

	return &SpecExtractor{
		agentClient: agentClient,
		logger:      logger,
		cfg:         cfg,
	}, nil
}

// ExtractSpec analyzes conversation and extracts canonical spec
func (e *SpecExtractor) ExtractSpec(conversation []Message, userIntent string) (*SpecExtractionResult, error) {
	// Validate inputs
	if userIntent == "" {
		return nil, fmt.Errorf("userIntent cannot be empty")
	}
	if len(conversation) == 0 {
		return nil, fmt.Errorf("conversation cannot be empty")
	}

	// Build conversation text for LLM
	var conversationText strings.Builder
	for _, msg := range conversation {
		conversationText.WriteString(fmt.Sprintf("%s: %s\n\n", msg.Role, msg.Content))
	}

	// Build the full prompt
	prompt := SpecExtractionPrompt()
	fullPrompt := fmt.Sprintf("%s\n\nConversation:\n%s\n\nUser's Primary Intent: %s",
		prompt, conversationText.String(), userIntent)

	// Call LLM to extract spec
	e.logger.LogProcessStep("Extracting canonical specification from conversation...")

	messages := []api.Message{
		{Role: "user", Content: fullPrompt},
	}

	chatResponse, err := e.agentClient.SendChatRequest(messages, nil, "")
	if err != nil {
		// Check for rate limiting or timeout errors
		errStr := err.Error()
		if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate limit") {
			return nil, fmt.Errorf("spec extraction rate limited - please retry later")
		}
		return nil, fmt.Errorf("failed to extract spec: %w", err)
	}

	response := chatResponse.Choices[0].Message.Content

	// Parse JSON response
	var result struct {
		Objective  string   `json:"objective"`
		InScope    []string `json:"in_scope"`
		OutOfScope []string `json:"out_of_scope"`
		Acceptance []string `json:"acceptance"`
		Context    string   `json:"context"`
		Confidence float64  `json:"confidence"`
		Reasoning  string   `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(response), &result); err != nil {
		// Try to extract JSON from response if it's wrapped in markdown code blocks
		jsonStart := strings.Index(response, "{")
		jsonEnd := strings.LastIndex(response, "}")
		if jsonStart >= 0 && jsonEnd > jsonStart {
			jsonStr := response[jsonStart : jsonEnd+1]
			if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
				return nil, fmt.Errorf("failed to parse spec JSON: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to parse spec JSON: %w", err)
		}
	}

	// Create canonical spec with unique ID
	specID := fmt.Sprintf("spec-%d-%d", time.Now().Unix(), rand.Intn(1000))
	spec := &CanonicalSpec{
		ID:           specID,
		CreatedAt:    time.Now(),
		UserPrompt:   userIntent,
		Objective:    result.Objective,
		InScope:      result.InScope,
		OutOfScope:   result.OutOfScope,
		Acceptance:   result.Acceptance,
		Context:      result.Context,
		Conversation: conversation,
	}

	extractionResult := &SpecExtractionResult{
		Spec:       spec,
		Confidence: result.Confidence,
		Reasoning:  result.Reasoning,
	}

	e.logger.LogProcessStep(fmt.Sprintf("Extracted spec with %.0f%% confidence", result.Confidence*100))
	e.logger.LogProcessStep(fmt.Sprintf("Objective: %s", result.Objective))
	e.logger.LogProcessStep(fmt.Sprintf("In scope: %d items, Out of scope: %d items",
		len(result.InScope), len(result.OutOfScope)))

	return extractionResult, nil
}

// UpdateSpec updates existing spec with new conversation
func (e *SpecExtractor) UpdateSpec(existing *CanonicalSpec, newMessages []Message) (*CanonicalSpec, error) {
	// Append new messages to conversation
	updatedConversation := append(existing.Conversation, newMessages...)

	// Extract new spec from full conversation
	result, err := e.ExtractSpec(updatedConversation, existing.UserPrompt)
	if err != nil {
		return nil, err
	}

	// Keep original ID and creation time, update everything else
	result.Spec.ID = existing.ID
	result.Spec.CreatedAt = existing.CreatedAt

	return result.Spec, nil
}
