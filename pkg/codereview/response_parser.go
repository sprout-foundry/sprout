package codereview

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// parseStructuredReviewResponse parses a structured JSON review response from the agent
func (s *CodeReviewService) parseStructuredReviewResponse(response *api.ChatResponse) (*types.CodeReviewResult, error) {
	if len(response.Choices) == 0 {
		return nil, errors.New("no response choices received from agent")
	}

	content := response.Choices[0].Message.Content
	candidates := extractStructuredReviewCandidates(content)
	for _, candidate := range candidates {
		if parsed, ok := parseStructuredReviewCandidate(candidate); ok {
			return parsed, nil
		}
	}

	// Fail closed for structured reviews to avoid accidental approvals.
	feedback := strings.TrimSpace(content)
	if feedback == "" {
		feedback = "Structured review returned no parseable JSON output."
	}
	return &types.CodeReviewResult{
		Status:   "needs_revision",
		Feedback: feedback,
	}, nil
}

func extractStructuredReviewCandidates(content string) []string {
	candidates := make([]string, 0, 4)
	seen := make(map[string]struct{})

	if jsonStr, err := utils.ExtractJSON(content); err == nil && strings.TrimSpace(jsonStr) != "" {
		jsonStr = strings.TrimSpace(jsonStr)
		seen[jsonStr] = struct{}{}
		candidates = append(candidates, jsonStr)
	}

	for _, part := range utils.SplitTopLevelJSONObjects(content) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		candidates = append(candidates, part)
	}

	return candidates
}

func parseStructuredReviewCandidate(candidate string) (*types.CodeReviewResult, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &raw); err != nil {
		return nil, false
	}

	reviewResult := &types.CodeReviewResult{}

	// status (required)
	if v, ok := raw["status"]; ok {
		if err := json.Unmarshal(v, &reviewResult.Status); err != nil {
			return nil, false
		}
		reviewResult.Status = normalizeStructuredReviewStatus(reviewResult.Status)
	}

	// feedback (required)
	if v, ok := raw["feedback"]; ok {
		if err := json.Unmarshal(v, &reviewResult.Feedback); err != nil {
			return nil, false
		}
	}

	// Optional bool fallback if status is omitted in rare cases
	if reviewResult.Status == "" {
		if v, ok := raw["approved"]; ok {
			var approved bool
			if err := json.Unmarshal(v, &approved); err == nil {
				if approved {
					reviewResult.Status = "approved"
				} else {
					reviewResult.Status = "needs_revision"
				}
			}
		}
	}

	// Optional fields
	if v, ok := raw["new_prompt"]; ok {
		_ = json.Unmarshal(v, &reviewResult.NewPrompt)
	}
	if v, ok := raw["detailed_guidance"]; ok {
		reviewResult.DetailedGuidance = parseDetailedGuidance(v)
	}

	// Ensure required fields are present and valid.
	switch reviewResult.Status {
	case "approved", "needs_revision", "rejected":
	default:
		return nil, false
	}
	if reviewResult.Feedback == "" {
		return nil, false
	}

	return reviewResult, true
}

func normalizeStructuredReviewStatus(status string) string {
	s := strings.ToLower(strings.TrimSpace(status))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	switch s {
	case "needsrevision":
		return "needs_revision"
	default:
		return s
	}
}

func parseDetailedGuidance(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}

	// Common case: string guidance.
	var guidanceText string
	if err := json.Unmarshal(trimmed, &guidanceText); err == nil {
		return strings.TrimSpace(guidanceText)
	}

	// Also accept object/array guidance and render it as indented JSON.
	var structured any
	if err := json.Unmarshal(trimmed, &structured); err != nil {
		return ""
	}
	pretty, err := json.MarshalIndent(structured, "", "  ")
	if err != nil {
		return ""
	}
	return string(pretty)
}

// parseHumanReadableReviewResponse parses a human-readable review response from the agent
func (s *CodeReviewService) parseHumanReadableReviewResponse(response *api.ChatResponse) (*types.CodeReviewResult, error) {
	if len(response.Choices) == 0 {
		return nil, errors.New("no response choices received from agent")
	}

	content := response.Choices[0].Message.Content
	// For staged reviews, we typically return the content as-is with approved status
	// unless there are clear rejection indicators
	status := "approved"
	if strings.Contains(strings.ToLower(content), "reject") || strings.Contains(strings.ToLower(content), "not acceptable") {
		status = "rejected"
	} else if strings.Contains(strings.ToLower(content), "needs") && strings.Contains(strings.ToLower(content), "revision") {
		status = "needs_revision"
	}

	return &types.CodeReviewResult{
		Status:   status,
		Feedback: content,
	}, nil
}
