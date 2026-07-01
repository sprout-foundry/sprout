package api

import "testing"

// TestOllamaLocalClient_SupportsConversationalVision distinguishes OCR-only
// models (glm-ocr) from inline-multimodal chat models (llama3.2-vision).
// SP-103-C2 requirement: clients that accept image input but produce
// extraction-style output (not chat-format) should be excluded from the
// conversational vision path.
func TestOllamaLocalClient_SupportsConversationalVision(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"glm-ocr", false},                   // OCR-only
		{"GLM-OCR", false},                   // case-insensitive
		{"vision-model", true},               // matches "vision" tag
		{"llama3.2-vision", true},            // multimodal chat
		{"Llama-3.2-11B-Vision", true},       // multimodal chat
		{"llama3.2", true},                   // multimodal chat
		{"gpt-oss:20b", false},               // plain text
		{"qwen2.5-coder:7b", false},          // plain text
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			c := &OllamaLocalClient{model: tc.model}
			if got := c.SupportsConversationalVision(); got != tc.want {
				t.Errorf("SupportsConversationalVision(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

// TestOllamaLocalClient_SupportsVision_AndConversational checks the
// relationship between SupportsVision and SupportsConversationalVision:
// vision-accepting models include OCR; conversational vision excludes OCR.
func TestOllamaLocalClient_SupportsVision_AndConversational(t *testing.T) {
	cases := []struct {
		model           string
		wantVision      bool
		wantConvoVision bool
	}{
		{"glm-ocr", true, false},
		{"llama3.2-vision", true, true},
		{"gpt-oss:20b", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			c := &OllamaLocalClient{model: tc.model}
			if got := c.SupportsVision(); got != tc.wantVision {
				t.Errorf("SupportsVision(%q) = %v, want %v", tc.model, got, tc.wantVision)
			}
			if got := c.SupportsConversationalVision(); got != tc.wantConvoVision {
				t.Errorf("SupportsConversationalVision(%q) = %v, want %v", tc.model, got, tc.wantConvoVision)
			}
		})
	}
}

// TestBaseProvider_SupportsConversationalVision defaults to true when
// supportsVision is set; OCR-only overrides are the caller's responsibility
// (OllamaLocalClient overrides this method directly).
func TestBaseProvider_SupportsConversationalVision(t *testing.T) {
	p := &BaseProvider{supportsVision: false}
	if p.SupportsConversationalVision() {
		t.Error("expected false when supportsVision is false")
	}
	p.supportsVision = true
	if !p.SupportsConversationalVision() {
		t.Error("expected true when supportsVision is true (default behavior)")
	}
}
