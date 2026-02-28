package codereview

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

func TestParseStructuredReviewResponse_FailClosedOnInvalidJSON(t *testing.T) {
	service := NewCodeReviewService(&configuration.Config{}, utils.GetLogger(true))
	response := &api.ChatResponse{
		Choices: []api.Choice{
			{
				Message: struct {
					Role             string          `json:"role"`
					Content          string          `json:"content"`
					ReasoningContent string          `json:"reasoning_content,omitempty"`
					Images           []api.ImageData `json:"images,omitempty"`
					ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
				}{Content: "this is not json"},
			},
		},
	}

	result, err := service.parseStructuredReviewResponse(response)
	if err != nil {
		t.Fatalf("parseStructuredReviewResponse returned error: %v", err)
	}
	if result.Status != "needs_revision" {
		t.Fatalf("expected status needs_revision, got %q", result.Status)
	}
}

func TestParseStructuredReviewResponse_ParsesValidJSONStatus(t *testing.T) {
	service := NewCodeReviewService(&configuration.Config{}, utils.GetLogger(true))
	response := &api.ChatResponse{
		Choices: []api.Choice{
			{
				Message: struct {
					Role             string          `json:"role"`
					Content          string          `json:"content"`
					ReasoningContent string          `json:"reasoning_content,omitempty"`
					Images           []api.ImageData `json:"images,omitempty"`
					ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
				}{Content: `{"status":"approved","feedback":"looks good"}`},
			},
		},
	}

	result, err := service.parseStructuredReviewResponse(response)
	if err != nil {
		t.Fatalf("parseStructuredReviewResponse returned error: %v", err)
	}
	if result.Status != "approved" {
		t.Fatalf("expected status approved, got %q", result.Status)
	}
}

func TestParseStructuredReviewResponse_PicksStructuredObjectFromMixedContent(t *testing.T) {
	service := NewCodeReviewService(&configuration.Config{}, utils.GetLogger(true))
	content := `analysis text with braces {not json}
{"status":"approved","feedback":"looks good","detailed_guidance":"{}"}
trailing text`
	response := &api.ChatResponse{
		Choices: []api.Choice{
			{
				Message: struct {
					Role             string          `json:"role"`
					Content          string          `json:"content"`
					ReasoningContent string          `json:"reasoning_content,omitempty"`
					Images           []api.ImageData `json:"images,omitempty"`
					ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
				}{Content: content},
			},
		},
	}

	result, err := service.parseStructuredReviewResponse(response)
	if err != nil {
		t.Fatalf("parseStructuredReviewResponse returned error: %v", err)
	}
	if result.Status != "approved" {
		t.Fatalf("expected status approved, got %q", result.Status)
	}
	if result.Feedback != "looks good" {
		t.Fatalf("expected feedback \"looks good\", got %q", result.Feedback)
	}
}
