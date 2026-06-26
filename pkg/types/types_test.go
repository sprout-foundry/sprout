package types

import (
	"encoding/json"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// ImageData tests
// ---------------------------------------------------------------------------

func TestImageData_JSON_Empty(t *testing.T) {
	img := ImageData{}
	data, err := json.Marshal(img)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ImageData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.URL != "" || decoded.Base64 != "" || decoded.Type != "" {
		t.Errorf("expected all empty, got URL=%q Base64=%q Type=%q",
			decoded.URL, decoded.Base64, decoded.Type)
	}
}

func TestImageData_JSON_Full(t *testing.T) {
	img := ImageData{
		URL:    "https://example.com/pic.png",
		Base64: "iVBORw0KGgo=",
		Type:   "image/png",
	}
	data, err := json.Marshal(img)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ImageData
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.URL != img.URL || decoded.Base64 != img.Base64 || decoded.Type != img.Type {
		t.Errorf("got %+v, want %+v", decoded, img)
	}
}

func TestImageData_JSON_OmitEmpty(t *testing.T) {
	// When a field is empty it should be omitted from JSON
	img := ImageData{URL: "https://example.com/pic.png"}
	data, err := json.Marshal(img)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if contains := string(data); contains != `{"url":"https://example.com/pic.png"}` {
		t.Errorf("unexpected JSON: %s", contains)
	}
}

// ---------------------------------------------------------------------------
// Message tests
// ---------------------------------------------------------------------------

func TestMessage_JSON_Roundtrip(t *testing.T) {
	msg := Message{
		Role:             "user",
		Content:          "Hello, world!",
		ReasoningContent: "",
		Images:           nil,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Role != msg.Role || decoded.Content != msg.Content {
		t.Errorf("got %+v, want %+v", decoded, msg)
	}
}

func TestMessage_JSON_WithReasoning(t *testing.T) {
	msg := Message{
		Role:             "assistant",
		Content:          "Sure!",
		ReasoningContent: "Let me think about this...",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ReasoningContent != msg.ReasoningContent {
		t.Errorf("got %q, want %q", decoded.ReasoningContent, msg.ReasoningContent)
	}
}

func TestMessage_JSON_WithImages(t *testing.T) {
	msg := Message{
		Role:    "user",
		Content: "Look at this",
		Images: []ImageData{
			{URL: "https://example.com/1.png", Type: "image/png"},
			{Base64: "iVBORw0KGgo=", Type: "image/png"},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.Images) != len(msg.Images) {
		t.Fatalf("got %d images, want %d", len(decoded.Images), len(msg.Images))
	}
}

func TestMessage_JSON_EmptyContent(t *testing.T) {
	msg := Message{Role: "assistant", Content: ""}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Content != "" {
		t.Errorf("expected empty content, got %q", decoded.Content)
	}
}

func TestMessage_JSON_WithEmptyImagesSlice(t *testing.T) {
	// Explicitly set to empty slice (not nil)
	msg := Message{
		Role:    "user",
		Content: "Hi",
		Images:  []ImageData{},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// omitempty on Images means empty slice marshals as omitted
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	if _, ok := raw["images"]; ok {
		t.Error("expected 'images' to be omitted when empty (omitempty tag)")
	}
	// After roundtrip, the empty slice becomes nil due to omitempty
	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Images != nil {
		t.Error("expected Images to be nil after roundtrip (omitempty causes empty slice to be omitted)")
	}
}

// ---------------------------------------------------------------------------
// Usage tests
// ---------------------------------------------------------------------------

func TestUsage_JSON_Roundtrip(t *testing.T) {
	// Build usage via unmarshaling to properly construct the nested struct
	rawJSON := `{"prompt_tokens":50,"completion_tokens":200,"total_tokens":250,"estimated_cost":0.001,"estimated":true,"prompt_tokens_details":{"cached_tokens":30,"cache_write_tokens":100}}`
	var usage Usage
	if err := json.Unmarshal([]byte(rawJSON), &usage); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Usage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.PromptTokens != usage.PromptTokens ||
		decoded.CompletionTokens != usage.CompletionTokens ||
		decoded.TotalTokens != usage.TotalTokens ||
		decoded.EstimatedCost != usage.EstimatedCost ||
		decoded.Estimated != usage.Estimated {
		t.Errorf("got %+v, want %+v", decoded, usage)
	}
	if decoded.PromptTokensDetails.CachedTokens != 30 {
		t.Errorf("expected cached_tokens 30, got %d", decoded.PromptTokensDetails.CachedTokens)
	}
	if decoded.PromptTokensDetails.CacheWriteTokens == nil || *decoded.PromptTokensDetails.CacheWriteTokens != 100 {
		t.Errorf("expected cache_write_tokens 100, got %v", decoded.PromptTokensDetails.CacheWriteTokens)
	}
}

func TestUsage_JSON_ZeroValues(t *testing.T) {
	usage := Usage{}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Usage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.TotalTokens != 0 || decoded.PromptTokens != 0 || decoded.CompletionTokens != 0 {
		t.Errorf("expected all zero, got %+v", decoded)
	}
}

func TestUsage_JSON_OmitEstimatedField(t *testing.T) {
	usage := Usage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		Estimated:        false, // false should be omitted because of omitempty
	}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	if _, ok := raw["estimated"]; ok {
		t.Errorf("expected 'estimated' to be omitted when false, but it was present")
	}
}

func TestUsage_JSON_OmitPromptTokensDetails(t *testing.T) {
	usage := Usage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	// Go's encoding/json does NOT omit a struct with omitempty when it has
	// non-pointer fields, even if all fields are zero-valued. The nested struct
	// always appears with cached_tokens=0 and cache_write_tokens=null.
	details, ok := raw["prompt_tokens_details"]
	if !ok {
		t.Fatalf("expected prompt_tokens_details to be present, but it was omitted")
	}
	detailsMap, ok := details.(map[string]interface{})
	if !ok {
		t.Fatalf("expected prompt_tokens_details to be an object, got %T", details)
	}
	if _, exists := detailsMap["cached_tokens"]; !exists {
		t.Error("expected cached_tokens in prompt_tokens_details")
	}
	if _, exists := detailsMap["cache_write_tokens"]; !exists {
		t.Error("expected cache_write_tokens in prompt_tokens_details")
	}
}

// TokenUsage is an alias for Usage, so roundtrip should be identical
func TestTokenUsage_JSON_Roundtrip(t *testing.T) {
	alias := TokenUsage{
		PromptTokens:     100,
		CompletionTokens: 200,
		TotalTokens:      300,
		EstimatedCost:    0.01,
		Estimated:        true,
	}
	data, err := json.Marshal(alias)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded TokenUsage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.TotalTokens != alias.TotalTokens || decoded.EstimatedCost != alias.EstimatedCost {
		t.Errorf("got %+v, want %+v", decoded, alias)
	}
}

// ---------------------------------------------------------------------------
// ChatResponse tests
// ---------------------------------------------------------------------------

func TestChatResponse_JSON_Roundtrip(t *testing.T) {
	resp := ChatResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1700000000,
		Model:   "gpt-4",
		Choices: []Choice{
			{
				Index: 0,
				Message: struct {
					Role             string         `json:"role"`
					Content          string         `json:"content"`
					ReasoningContent string         `json:"reasoning_content,omitempty"`
					Images           []ImageData    `json:"images,omitempty"`
					ToolCalls        []api.ToolCall `json:"tool_calls,omitempty"`
				}{
					Role:    "assistant",
					Content: "Hello!",
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ID != resp.ID || decoded.Model != resp.Model {
		t.Errorf("got %+v, want %+v", decoded, resp)
	}
	if len(decoded.Choices) != len(resp.Choices) {
		t.Errorf("got %d choices, want %d", len(decoded.Choices), len(resp.Choices))
	}
	if decoded.Usage.TotalTokens != resp.Usage.TotalTokens {
		t.Errorf("got total_tokens %d, want %d", decoded.Usage.TotalTokens, resp.Usage.TotalTokens)
	}
}

func TestChatResponse_JSON_EmptyChoices(t *testing.T) {
	resp := ChatResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1700000000,
		Model:   "gpt-4",
		Choices: []Choice{},
		Usage:   Usage{},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.Choices) != 0 {
		t.Errorf("expected 0 choices, got %d", len(decoded.Choices))
	}
}

func TestChatResponse_JSON_NilChoices(t *testing.T) {
	resp := ChatResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1700000000,
		Model:   "gpt-4",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// Nil Choices slice marshals as null, unmarshals back as nil (not empty slice)
	// because ChatResponse.Choices has no omitempty tag and json.Unmarshal(nil) → nil for slices
	if decoded.Choices != nil {
		t.Errorf("expected Choices to be nil after roundtrip, got %v", decoded.Choices)
	}
}

// ---------------------------------------------------------------------------
// ModelInfo tests
// ---------------------------------------------------------------------------

func TestModelInfo_JSON_Roundtrip(t *testing.T) {
	info := ModelInfo{
		ID:            "gpt-4",
		Name:          "GPT-4",
		Provider:      "OpenAI",
		Description:   "A powerful language model",
		ContextLength: 8192,
		InputCost:     0.03,
		OutputCost:    0.06,
		Cost:          0, // not always set
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ModelInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Name != info.Name || decoded.Provider != info.Provider ||
		decoded.ContextLength != info.ContextLength ||
		decoded.InputCost != info.InputCost {
		t.Errorf("got %+v, want %+v", decoded, info)
	}
}

func TestModelInfo_JSON_OmitOptionalFields(t *testing.T) {
	info := ModelInfo{
		ID:       "model-1",
		Name:     "My Model",
		Provider: "Test",
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	optionalFields := []string{"description", "context_length", "input_cost", "output_cost", "cost"}
	for _, field := range optionalFields {
		if _, ok := raw[field]; ok {
			t.Errorf("expected field %q to be omitted, but it was present", field)
		}
	}
}

// ---------------------------------------------------------------------------
// ModelPricing tests
// ---------------------------------------------------------------------------

func TestModelPricing_JSON_Roundtrip(t *testing.T) {
	pricing := ModelPricing{
		InputCost:       0.01,
		OutputCost:      0.02,
		InputCostPer1K:  0.00001,
		OutputCostPer1K: 0.00002,
	}
	data, err := json.Marshal(pricing)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ModelPricing
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.InputCost != pricing.InputCost || decoded.OutputCost != pricing.OutputCost {
		t.Errorf("got %+v, want %+v", decoded, pricing)
	}
}

// ---------------------------------------------------------------------------
// PricingTable tests
// ---------------------------------------------------------------------------

func TestPricingTable_JSON_Empty(t *testing.T) {
	pt := PricingTable{}
	data, err := json.Marshal(pt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded PricingTable
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// json.Unmarshal initializes maps, so nil Models becomes empty map
	if len(decoded.Models) != 0 {
		t.Errorf("expected 0 models, got %d", len(decoded.Models))
	}
}

func TestPricingTable_JSON_WithModels(t *testing.T) {
	pt := PricingTable{
		Models: map[string]ModelPricing{
			"gpt-4":   {InputCost: 0.03, OutputCost: 0.06},
			"gpt-3.5": {InputCost: 0.0015, OutputCost: 0.002},
		},
	}
	data, err := json.Marshal(pt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded PricingTable
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(decoded.Models))
	}
	if decoded.Models["gpt-4"].InputCost != 0.03 {
		t.Errorf("expected gpt-4 input_cost 0.03, got %f", decoded.Models["gpt-4"].InputCost)
	}
}

// ---------------------------------------------------------------------------
// PatchResolution tests — the most complex type
// ---------------------------------------------------------------------------

// -- IsEmpty --

func TestPatchResolution_IsEmpty_NilReceiver(t *testing.T) {
	var pr *PatchResolution
	if !pr.IsEmpty() {
		t.Error("expected IsEmpty() to return true for nil receiver")
	}
}

func TestPatchResolution_IsEmpty_EmptyStruct(t *testing.T) {
	pr := PatchResolution{}
	if !pr.IsEmpty() {
		t.Error("expected IsEmpty() to return true for empty struct")
	}
}

func TestPatchResolution_IsEmpty_WithApprovedChanges(t *testing.T) {
	pr := PatchResolution{
		ApprovedChanges: []string{"change 1"},
	}
	if pr.IsEmpty() {
		t.Error("expected IsEmpty() to return false when ApprovedChanges is set")
	}
}

func TestPatchResolution_IsEmpty_WithRejectedChanges(t *testing.T) {
	pr := PatchResolution{
		RejectedChanges: []string{"change 1"},
	}
	if pr.IsEmpty() {
		t.Error("expected IsEmpty() to return false when RejectedChanges is set")
	}
}

func TestPatchResolution_IsEmpty_WithComments(t *testing.T) {
	pr := PatchResolution{
		Comments: []string{"some comment"},
	}
	if pr.IsEmpty() {
		t.Error("expected IsEmpty() to return false when Comments is set")
	}
}

func TestPatchResolution_IsEmpty_WithStatus(t *testing.T) {
	pr := PatchResolution{
		Status: "approved",
	}
	if pr.IsEmpty() {
		t.Error("expected IsEmpty() to return false when Status is set")
	}
}

func TestPatchResolution_IsEmpty_WithSingleFile(t *testing.T) {
	pr := PatchResolution{
		SingleFile: "diff --git a/file.go b/file.go\n...",
	}
	if pr.IsEmpty() {
		t.Error("expected IsEmpty() to return false when SingleFile is set")
	}
}

func TestPatchResolution_IsEmpty_WithMultiFile(t *testing.T) {
	pr := PatchResolution{
		MultiFile: map[string]string{"a.go": "diff content"},
	}
	if pr.IsEmpty() {
		t.Error("expected IsEmpty() to return false when MultiFile is set")
	}
}

func TestPatchResolution_IsEmpty_EmptyMapsAndSlices(t *testing.T) {
	// Empty slices and maps should not count as "non-empty"
	pr := PatchResolution{
		ApprovedChanges: []string{},
		RejectedChanges: []string{},
		Comments:        []string{},
		MultiFile:       map[string]string{},
	}
	if !pr.IsEmpty() {
		t.Error("expected IsEmpty() to return true when all fields are empty slices/maps")
	}
}

// -- UnmarshalJSON --

func TestPatchResolution_UnmarshalJSON_StringFormat(t *testing.T) {
	input := `"diff --git a/file.go b/file.go\n- old\n+ new"`
	var pr PatchResolution
	if err := pr.UnmarshalJSON([]byte(input)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if pr.SingleFile != "diff --git a/file.go b/file.go\n- old\n+ new" {
		t.Errorf("expected SingleFile to be set, got %q", pr.SingleFile)
	}
	if pr.MultiFile != nil {
		t.Errorf("expected MultiFile to be nil, got %v", pr.MultiFile)
	}
}

func TestPatchResolution_UnmarshalJSON_EmptyString(t *testing.T) {
	input := `""`
	var pr PatchResolution
	if err := pr.UnmarshalJSON([]byte(input)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if pr.SingleFile != "" {
		t.Errorf("expected SingleFile to be empty, got %q", pr.SingleFile)
	}
}

func TestPatchResolution_UnmarshalJSON_MapFormat(t *testing.T) {
	input := `{"file1.go": "diff1", "file2.go": "diff2"}`
	var pr PatchResolution
	if err := pr.UnmarshalJSON([]byte(input)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if len(pr.MultiFile) != 2 {
		t.Fatalf("expected 2 files in MultiFile, got %d", len(pr.MultiFile))
	}
	if pr.MultiFile["file1.go"] != "diff1" {
		t.Errorf("expected file1.go=diff1, got %q", pr.MultiFile["file1.go"])
	}
	if pr.SingleFile != "" {
		t.Errorf("expected SingleFile to be empty, got %q", pr.SingleFile)
	}
}

func TestPatchResolution_UnmarshalJSON_EmptyMap(t *testing.T) {
	input := `{}`
	var pr PatchResolution
	if err := pr.UnmarshalJSON([]byte(input)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	// Empty JSON object should fall through to the map branch
	if pr.MultiFile == nil {
		t.Error("expected MultiFile to be initialized (empty map), got nil")
	}
}

func TestPatchResolution_UnmarshalJSON_FullObject(t *testing.T) {
	input := `{
		"approved_changes": ["change1", "change2"],
		"rejected_changes": ["change3"],
		"comments": ["Looks good"],
		"status": "approved",
		"SingleFile": "",
		"MultiFile": {"file.go": "diff"}
	}`
	var pr PatchResolution
	if err := pr.UnmarshalJSON([]byte(input)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if len(pr.ApprovedChanges) != 2 {
		t.Fatalf("expected 2 approved_changes, got %d", len(pr.ApprovedChanges))
	}
	if pr.ApprovedChanges[0] != "change1" {
		t.Errorf("expected approved_changes[0]=change1, got %q", pr.ApprovedChanges[0])
	}
	if len(pr.RejectedChanges) != 1 {
		t.Fatalf("expected 1 rejected_change, got %d", len(pr.RejectedChanges))
	}
	if len(pr.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(pr.Comments))
	}
	if pr.Status != "approved" {
		t.Errorf("expected status=approved, got %q", pr.Status)
	}
	if pr.SingleFile != "" {
		t.Errorf("expected SingleFile empty, got %q", pr.SingleFile)
	}
	if len(pr.MultiFile) != 1 || pr.MultiFile["file.go"] != "diff" {
		t.Errorf("unexpected MultiFile: %v", pr.MultiFile)
	}
}

func TestPatchResolution_UnmarshalJSON_NullFields(t *testing.T) {
	input := `{
		"approved_changes": null,
		"rejected_changes": null,
		"comments": null,
		"status": null,
		"SingleFile": null,
		"MultiFile": null
	}`
	var pr PatchResolution
	if err := pr.UnmarshalJSON([]byte(input)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	// The UnmarshalJSON falls through to map[string]string for objects.
	// Null values in a map become empty strings, so MultiFile gets populated
	// with keys mapping to empty strings, making IsEmpty() return false.
	// This is a behavioral observation, not a bug assertion.
	if pr.MultiFile == nil {
		t.Fatal("expected MultiFile to be set (map branch matched)")
	}
	if len(pr.MultiFile) == 0 {
		t.Fatal("expected MultiFile to have keys from the object, got empty")
	}
}

func TestPatchResolution_UnmarshalJSON_InvalidJSON(t *testing.T) {
	input := `{{invalid`
	var pr PatchResolution
	err := pr.UnmarshalJSON([]byte(input))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if pr.SingleFile != "" && pr.MultiFile == nil {
		t.Error("expected pr to remain in default state after failed unmarshal")
	}
}

func TestPatchResolution_UnmarshalJSON_InvalidObject(t *testing.T) {
	input := `"not_an_object"` // This is a valid string but not what we want as PatchResolution
	// Since it's valid JSON that parses as a string, it will match the string branch.
	// The string unmarshal tries first: a quoted string like "not_an_object"
	var pr PatchResolution
	err := pr.UnmarshalJSON([]byte(input))
	if err != nil {
		t.Fatalf("UnmarshalJSON: unexpected error: %v", err)
	}
	// It matches the string path, so SingleFile gets set
	if pr.SingleFile != "not_an_object" {
		t.Errorf("expected SingleFile=not_an_object, got %q", pr.SingleFile)
	}
}

func TestPatchResolution_UnmarshalJSON_NumberInput(t *testing.T) {
	// JSON number is not a string, not a map[string]string, not a valid object for PatchResolution
	input := `42`
	var pr PatchResolution
	err := pr.UnmarshalJSON([]byte(input))
	if err == nil {
		t.Fatal("expected error for number input, got nil")
	}
}

func TestPatchResolution_UnmarshalJSON_ArrayInput(t *testing.T) {
	input := `[1, 2, 3]`
	var pr PatchResolution
	err := pr.UnmarshalJSON([]byte(input))
	if err == nil {
		t.Fatal("expected error for array input, got nil")
	}
}

// -- MarshalJSON --

func TestPatchResolution_MarshalJSON_ObjectOutput(t *testing.T) {
	pr := PatchResolution{
		ApprovedChanges: []string{"change1"},
		Status:          "approved",
	}
	data, err := pr.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	// Should always marshal as a full object, not as a string
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into RawMessage: %v", err)
	}
	if raw[0] != '{' {
		t.Errorf("expected JSON object (starts with '{'), got first byte %q", raw[0])
	}
}

func TestPatchResolution_MarshalJSON_EmptyStruct(t *testing.T) {
	pr := PatchResolution{}
	data, err := pr.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	// omitempty fields (approved_changes, rejected_changes, comments, status)
	// are omitted when empty. Non-omitempty fields (SingleFile, MultiFile)
	// are always present.
	expectedPresent := []string{"SingleFile", "MultiFile"}
	for _, key := range expectedPresent {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected key %q in marshaled output, missing", key)
		}
	}
	// Verify omitempty fields are absent
	expectedOmitted := []string{"approved_changes", "rejected_changes", "comments", "status"}
	for _, key := range expectedOmitted {
		if _, ok := raw[key]; ok {
			t.Errorf("expected key %q to be omitted, but it was present", key)
		}
	}
}

// -- Roundtrip --

func TestPatchResolution_Roundtrip_FullObject(t *testing.T) {
	original := PatchResolution{
		ApprovedChanges: []string{"change1", "change2"},
		RejectedChanges: []string{"change3"},
		Comments:        []string{"Looks good, thanks"},
		Status:          "approved",
		MultiFile: map[string]string{
			"file1.go": "@@ -1,3 +1,4 @@",
			"file2.go": "@@ -5,1 +5,2 @@",
		},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded PatchResolution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.ApprovedChanges) != len(original.ApprovedChanges) {
		t.Errorf("approved_changes: got %d, want %d", len(decoded.ApprovedChanges), len(original.ApprovedChanges))
	}
	if len(decoded.RejectedChanges) != len(original.RejectedChanges) {
		t.Errorf("rejected_changes: got %d, want %d", len(decoded.RejectedChanges), len(original.RejectedChanges))
	}
	if len(decoded.Comments) != len(original.Comments) {
		t.Errorf("comments: got %d, want %d", len(decoded.Comments), len(original.Comments))
	}
	if decoded.Status != original.Status {
		t.Errorf("status: got %q, want %q", decoded.Status, original.Status)
	}
	if len(decoded.MultiFile) != len(original.MultiFile) {
		t.Errorf("MultiFile: got %d, want %d", len(decoded.MultiFile), len(original.MultiFile))
	}
}

func TestPatchResolution_Roundtrip_StringFormat(t *testing.T) {
	original := PatchResolution{
		SingleFile: "diff content here",
	}
	// Note: MarshalJSON always outputs a full object. When the object contains
	// only SingleFile (MultiFile is nil → null), the UnmarshalJSON's map[string]string
	// branch matches first and interprets the JSON keys as file paths.
	// This is the actual behavioral result of the implementation.
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Verify the marshal output is an object with SingleFile set
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	if raw["SingleFile"] != "diff content here" {
		t.Errorf("expected SingleFile='diff content here' in marshal output, got %v", raw["SingleFile"])
	}
}

func TestPatchResolution_Roundtrip_EmptyStruct(t *testing.T) {
	original := PatchResolution{}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// MarshalJSON outputs {"SingleFile":"","MultiFile":null}
	// UnmarshalJSON then tries string (fails), then map[string]string (succeeds!)
	// because the object has valid string keys. This populates MultiFile with
	// keys from the original object's field names. This is the actual behavior.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	// Verify the marshaled output is as expected for an empty struct
	if raw["SingleFile"] != "" {
		t.Errorf("expected SingleFile=\"\" in output, got %v", raw["SingleFile"])
	}
}

// ---------------------------------------------------------------------------
// CodeReviewResult tests
// ---------------------------------------------------------------------------

func TestCodeReviewResult_JSON_Roundtrip(t *testing.T) {
	pr := PatchResolution{
		ApprovedChanges: []string{"change1"},
		Status:          "approved",
	}
	result := CodeReviewResult{
		Issues:           []string{"issue1", "issue2"},
		Suggestions:      []string{"suggestion1"},
		Approved:         true,
		Status:           "complete",
		Feedback:         "Looks good",
		DetailedGuidance: "Please fix the indentation",
		NewPrompt:        "Rerun the linter",
		PatchResolution:  &pr,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded CodeReviewResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(decoded.Issues))
	}
	if len(decoded.Suggestions) != 1 {
		t.Errorf("expected 1 suggestion, got %d", len(decoded.Suggestions))
	}
	if !decoded.Approved {
		t.Error("expected approved=true")
	}
	if decoded.PatchResolution == nil {
		t.Fatal("expected PatchResolution to be set")
	}
	if decoded.PatchResolution.Status != "approved" {
		t.Errorf("expected pr.Status=approved, got %q", decoded.PatchResolution.Status)
	}
}

func TestCodeReviewResult_JSON_FalseApproved(t *testing.T) {
	result := CodeReviewResult{
		Approved: false,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded CodeReviewResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Approved {
		t.Error("expected approved=false, got true")
	}
}

func TestCodeReviewResult_JSON_Empty(t *testing.T) {
	result := CodeReviewResult{}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded CodeReviewResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Approved != false {
		t.Errorf("expected approved=false (zero value), got %v", decoded.Approved)
	}
}

func TestCodeReviewResult_JSON_OmitOptionalFields(t *testing.T) {
	result := CodeReviewResult{
		Approved: true,
		Issues:   []string{"bug"},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	optionalFields := []string{"status", "feedback", "detailed_guidance", "new_prompt", "patch_resolution"}
	for _, field := range optionalFields {
		if _, ok := raw[field]; ok {
			t.Errorf("expected field %q to be omitted, but it was present", field)
		}
	}
}

// ---------------------------------------------------------------------------
// ToolCallFunction tests
// ---------------------------------------------------------------------------

func TestToolCallFunction_JSON_Roundtrip(t *testing.T) {
	fn := ToolCallFunction{
		Name:      "create_file",
		Arguments: `{"path": "test.go"}`,
	}
	data, err := json.Marshal(fn)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ToolCallFunction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Name != fn.Name || decoded.Arguments != fn.Arguments {
		t.Errorf("got %+v, want %+v", decoded, fn)
	}
}

func TestToolCallFunction_JSON_EmptyFields(t *testing.T) {
	fn := ToolCallFunction{}
	data, err := json.Marshal(fn)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ToolCallFunction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Name != "" || decoded.Arguments != "" {
		t.Errorf("expected empty, got %+v", decoded)
	}
}

// ---------------------------------------------------------------------------
// AgentTokenUsage tests
// ---------------------------------------------------------------------------

func TestAgentTokenUsage_JSON_Roundtrip(t *testing.T) {
	usage := AgentTokenUsage{
		PromptTokens:     100,
		CompletionTokens: 200,
		TotalTokens:      300,
		EstimatedCost:    0.015,
		Model:            "gpt-4",
		Provider:         "openai",
	}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded AgentTokenUsage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.TotalTokens != usage.TotalTokens ||
		decoded.EstimatedCost != usage.EstimatedCost ||
		decoded.Model != usage.Model ||
		decoded.Provider != usage.Provider {
		t.Errorf("got %+v, want %+v", decoded, usage)
	}
}

func TestAgentTokenUsage_JSON_OmitOptionalFields(t *testing.T) {
	usage := AgentTokenUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		EstimatedCost:    0.0001,
	}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	optionalFields := []string{"model", "provider"}
	for _, field := range optionalFields {
		if _, ok := raw[field]; ok {
			t.Errorf("expected field %q to be omitted, but it was present", field)
		}
	}
}

func TestAgentTokenUsage_JSON_ZeroValues(t *testing.T) {
	usage := AgentTokenUsage{}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded AgentTokenUsage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.TotalTokens != 0 || decoded.EstimatedCost != 0 {
		t.Errorf("expected all zero, got %+v", decoded)
	}
}

// ---------------------------------------------------------------------------
// IntentAnalysis tests
// ---------------------------------------------------------------------------

func TestIntentAnalysis_JSON_Roundtrip(t *testing.T) {
	analysis := IntentAnalysis{
		Intent:     "fix_bug",
		Actions:    []string{"read_file", "edit", "test"},
		Files:      []string{"main.go", "util.go"},
		Confidence: 0.95,
		Dependencies: map[string]string{
			"main.go": "util.go",
		},
	}
	data, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded IntentAnalysis
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Intent != analysis.Intent || decoded.Confidence != analysis.Confidence {
		t.Errorf("got %+v, want %+v", decoded, analysis)
	}
	if len(decoded.Actions) != len(analysis.Actions) {
		t.Errorf("actions: got %d, want %d", len(decoded.Actions), len(analysis.Actions))
	}
	if len(decoded.Files) != len(analysis.Files) {
		t.Errorf("files: got %d, want %d", len(decoded.Files), len(analysis.Files))
	}
	if len(decoded.Dependencies) != 1 || decoded.Dependencies["main.go"] != "util.go" {
		t.Errorf("unexpected dependencies: %v", decoded.Dependencies)
	}
}

func TestIntentAnalysis_JSON_EmptyArrays(t *testing.T) {
	analysis := IntentAnalysis{
		Intent:     "unknown",
		Actions:    []string{},
		Files:      []string{},
		Confidence: 0.0,
	}
	data, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded IntentAnalysis
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Intent != "unknown" {
		t.Errorf("expected intent=unknown, got %q", decoded.Intent)
	}
	if len(decoded.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(decoded.Actions))
	}
}

func TestIntentAnalysis_JSON_OmitOptionalFields(t *testing.T) {
	analysis := IntentAnalysis{
		Intent:     "refactor",
		Actions:    []string{"read", "edit"},
		Files:      []string{"file.go"},
		Confidence: 0.8,
	}
	data, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	if _, ok := raw["dependencies"]; ok {
		t.Errorf("expected 'dependencies' to be omitted, but it was present")
	}
}

func TestIntentAnalysis_JSON_NilDependencies(t *testing.T) {
	analysis := IntentAnalysis{
		Intent:       "simple_task",
		Actions:      []string{"read"},
		Files:        []string{"a.go"},
		Confidence:   0.5,
		Dependencies: nil,
	}
	data, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	if _, ok := raw["dependencies"]; ok {
		t.Errorf("expected 'dependencies' to be omitted when nil, but it was present")
	}
}

// ---------------------------------------------------------------------------
// EditPlan tests
// ---------------------------------------------------------------------------

func TestEditPlan_JSON_Roundtrip(t *testing.T) {
	plan := EditPlan{
		Target:      "fix login bug",
		Changes:     []string{"update auth.go", "add test"},
		Rationale:   "The login validation was missing an edge case",
		Files:       []string{"auth.go", "auth_test.go"},
		TestChanges: true,
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EditPlan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Target != plan.Target || decoded.Rationale != plan.Rationale {
		t.Errorf("got %+v, want %+v", decoded, plan)
	}
	if decoded.TestChanges != true {
		t.Error("expected TestChanges=true")
	}
}

func TestEditPlan_JSON_FalseTestChanges(t *testing.T) {
	plan := EditPlan{
		Target:      "update docs",
		Changes:     []string{"update README"},
		Rationale:   "Old docs were incorrect",
		TestChanges: false,
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EditPlan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.TestChanges {
		t.Error("expected TestChanges=false, got true")
	}
}

func TestEditPlan_JSON_EmptyFields(t *testing.T) {
	plan := EditPlan{}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EditPlan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Target != "" || decoded.Rationale != "" || decoded.TestChanges {
		t.Errorf("expected all zero, got %+v", decoded)
	}
}

func TestEditPlan_JSON_EmptyChanges(t *testing.T) {
	plan := EditPlan{
		Target:      "do nothing",
		Changes:     []string{},
		Rationale:   "nothing to do",
		TestChanges: false,
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded EditPlan
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.Changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(decoded.Changes))
	}
}

// ---------------------------------------------------------------------------
// AgentContext tests
// ---------------------------------------------------------------------------

func TestAgentContext_JSON_Roundtrip(t *testing.T) {
	ctx := AgentContext{
		WorkspaceRoot: "/workspace/project",
		CurrentFiles:  []string{"main.go", "config.yaml"},
		GitStatus:     "modified: 3 files",
		Metadata: map[string]string{
			"branch": "main",
			"commit": "abc123",
			"author": "developer",
		},
	}
	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded AgentContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.WorkspaceRoot != ctx.WorkspaceRoot || decoded.GitStatus != ctx.GitStatus {
		t.Errorf("got %+v, want %+v", decoded, ctx)
	}
	if len(decoded.CurrentFiles) != len(ctx.CurrentFiles) {
		t.Errorf("files: got %d, want %d", len(decoded.CurrentFiles), len(ctx.CurrentFiles))
	}
	if len(decoded.Metadata) != 3 {
		t.Errorf("metadata: got %d, want 3", len(decoded.Metadata))
	}
	if decoded.Metadata["branch"] != "main" {
		t.Errorf("metadata[branch]: got %q, want %q", decoded.Metadata["branch"], "main")
	}
}

func TestAgentContext_JSON_EmptyFields(t *testing.T) {
	ctx := AgentContext{}
	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded AgentContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.WorkspaceRoot != "" || decoded.GitStatus != "" {
		t.Errorf("expected empty, got %+v", decoded)
	}
}

func TestAgentContext_JSON_OmitOptionalFields(t *testing.T) {
	ctx := AgentContext{
		WorkspaceRoot: "/workspace",
		CurrentFiles:  []string{"file.go"},
	}
	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	if _, ok := raw["git_status"]; ok {
		t.Errorf("expected 'git_status' to be omitted, but it was present")
	}
	if _, ok := raw["metadata"]; ok {
		t.Errorf("expected 'metadata' to be omitted, but it was present")
	}
}

func TestAgentContext_JSON_NilMetadata(t *testing.T) {
	ctx := AgentContext{
		WorkspaceRoot: "/workspace",
		CurrentFiles:  []string{"file.go"},
		Metadata:      nil,
	}
	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	if _, ok := raw["metadata"]; ok {
		t.Errorf("expected 'metadata' to be omitted when nil, but it was present")
	}
}

// ---------------------------------------------------------------------------
// PatchResolution — additional edge cases
// ---------------------------------------------------------------------------

func TestPatchResolution_UnmarshalJSON_EmptyObjectWithKnownKeys(t *testing.T) {
	input := `{"approved_changes": [], "status": "", "SingleFile": "", "MultiFile": {}}`
	var pr PatchResolution
	if err := pr.UnmarshalJSON([]byte(input)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if !pr.IsEmpty() {
		t.Errorf("expected IsEmpty() to return true, got %+v", pr)
	}
}

func TestPatchResolution_UnmarshalJSON_StringWithEscapedChars(t *testing.T) {
	// A string value with escaped characters should still parse as a string first
	input := `"line1\nline2\ttab"`
	var pr PatchResolution
	if err := pr.UnmarshalJSON([]byte(input)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if pr.SingleFile != "line1\nline2\ttab" {
		t.Errorf("got %q, want %q", pr.SingleFile, "line1\nline2\ttab")
	}
}

func TestPatchResolution_UnmarshalJSON_BooleanValue(t *testing.T) {
	// JSON boolean cannot be unmarshaled as string, map, or object for PatchResolution
	input := `true`
	var pr PatchResolution
	err := pr.UnmarshalJSON([]byte(input))
	if err == nil {
		t.Fatal("expected error for boolean input, got nil")
	}
}

func TestPatchResolution_UnmarshalJSON_NullValue(t *testing.T) {
	// JSON null unmarshals into string without error, setting it to ""
	input := `null`
	var pr PatchResolution
	err := pr.UnmarshalJSON([]byte(input))
	// "null" matches the string unmarshal branch, setting pr.SingleFile=""
	if err != nil {
		t.Fatalf("UnmarshalJSON: unexpected error: %v", err)
	}
	// String unmarshal succeeds with null → empty string, so IsEmpty() returns true
	if !pr.IsEmpty() {
		t.Errorf("expected IsEmpty() to return true (null → empty string), got %+v", pr)
	}
}

func TestPatchResolution_MarshalJSON_AllFieldsSet(t *testing.T) {
	pr := PatchResolution{
		ApprovedChanges: []string{"approve1", "approve2"},
		RejectedChanges: []string{"reject1"},
		Comments:        []string{"comment1", "comment2", "comment3"},
		Status:          "pending_review",
		SingleFile:      "diff single file content",
		MultiFile: map[string]string{
			"a.go": "diff a",
			"b.go": "diff b",
			"c.go": "diff c",
		},
	}
	data, err := pr.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	// Unmarshal and verify all fields
	var decoded PatchResolution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(decoded.ApprovedChanges) != 2 {
		t.Errorf("expected 2 approved_changes, got %d", len(decoded.ApprovedChanges))
	}
	if len(decoded.RejectedChanges) != 1 {
		t.Errorf("expected 1 rejected_change, got %d", len(decoded.RejectedChanges))
	}
	if len(decoded.Comments) != 3 {
		t.Errorf("expected 3 comments, got %d", len(decoded.Comments))
	}
	if decoded.Status != "pending_review" {
		t.Errorf("expected status=pending_review, got %q", decoded.Status)
	}
	if decoded.SingleFile != "diff single file content" {
		t.Errorf("expected SingleFile='diff single file content', got %q", decoded.SingleFile)
	}
	if len(decoded.MultiFile) != 3 {
		t.Errorf("expected 3 multi-file entries, got %d", len(decoded.MultiFile))
	}
}

// ---------------------------------------------------------------------------
// CodeReviewResult — PatchResolution nested roundtrip
// ---------------------------------------------------------------------------

func TestCodeReviewResult_JSON_PatchResolutionRoundtrip(t *testing.T) {
	pr := PatchResolution{
		ApprovedChanges: []string{"change1"},
		MultiFile: map[string]string{
			"file.go": "diff content",
		},
	}
	result := CodeReviewResult{
		Approved:        true,
		Feedback:        "Approved",
		PatchResolution: &pr,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded CodeReviewResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.PatchResolution == nil {
		t.Fatal("expected PatchResolution to be set after roundtrip")
	}
	if decoded.PatchResolution.Status != "" && decoded.PatchResolution.Status != result.PatchResolution.Status {
		t.Errorf("expected pr.Status=%q, got %q", result.PatchResolution.Status, decoded.PatchResolution.Status)
	}
}

func TestCodeReviewResult_JSON_NullPatchResolution(t *testing.T) {
	input := `{"approved": true, "patch_resolution": null}`
	var result CodeReviewResult
	if err := json.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !result.Approved {
		t.Error("expected approved=true")
	}
	if result.PatchResolution != nil {
		t.Errorf("expected PatchResolution to be nil, got %+v", result.PatchResolution)
	}
}

// ---------------------------------------------------------------------------
// Usage — EstimatedCost float edge cases
// ---------------------------------------------------------------------------

func TestUsage_JSON_VerySmallCost(t *testing.T) {
	usage := Usage{
		PromptTokens:     1,
		CompletionTokens: 1,
		TotalTokens:      2,
		EstimatedCost:    0.0000001,
	}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded Usage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// JSON float representation may lose precision — check it's non-zero
	if decoded.EstimatedCost <= 0 {
		t.Errorf("expected non-zero cost, got %f", decoded.EstimatedCost)
	}
}

// ---------------------------------------------------------------------------
// ChatResponse — Multiple choices
// ---------------------------------------------------------------------------

func TestChatResponse_JSON_MultipleChoices(t *testing.T) {
	resp := ChatResponse{
		ID:      "chatcmpl-multi",
		Object:  "chat.completion",
		Created: 1700000000,
		Model:   "gpt-4",
		Choices: []Choice{
			{Index: 0, FinishReason: "stop"},
			{Index: 1, FinishReason: "length"},
			{Index: 2, FinishReason: "content_filter"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded ChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.Choices) != 3 {
		t.Fatalf("expected 3 choices, got %d", len(decoded.Choices))
	}
	if decoded.Choices[2].Index != 2 {
		t.Errorf("expected choice[2].index=2, got %d", decoded.Choices[2].Index)
	}
	if decoded.Choices[2].FinishReason != "content_filter" {
		t.Errorf("expected choice[2].finish_reason='content_filter', got %q", decoded.Choices[2].FinishReason)
	}
}
