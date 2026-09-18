package api

import "testing"

// TestOllamaLocalClient_SupportsVision checks the OCR-only client's declared
// vision support: vision-accepting models include OCR models. SP-140 removed
// the separate conversational-vision distinction — declared vision flows
// inline like any other vision model.
func TestOllamaLocalClient_SupportsVision(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"glm-ocr", true},              // OCR-only, but accepts images
		{"GLM-OCR", true},              // case-insensitive
		{"vision-model", true},         // matches "vision" tag
		{"llama3.2-vision", true},      // multimodal chat
		{"Llama-3.2-11B-Vision", true}, // multimodal chat
		{"llama3.2", true},             // multimodal chat
		{"gpt-oss:20b", false},         // plain text
		{"qwen2.5-coder:7b", false},    // plain text
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			c := &OllamaLocalClient{model: tc.model}
			if got := c.SupportsVision(); got != tc.want {
				t.Errorf("SupportsVision(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}
