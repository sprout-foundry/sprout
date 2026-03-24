package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestPrepareMessages_StripsHistoricalImagesForNonVision(t *testing.T) {
	a := &Agent{
		client:       &visionSupportingClient{supportsVision: false},
		systemPrompt: "system",
		messages: []api.Message{
			{
				Role:    "user",
				Content: "old image message",
				Images: []api.ImageData{
					{Base64: "ZmFrZQ==", Type: "image/png"},
				},
			},
			{
				Role:    "assistant",
				Content: "ok",
			},
		},
	}

	handler := NewConversationHandler(a)
	prepared := handler.prepareMessages(nil)
	for _, msg := range prepared {
		if len(msg.Images) != 0 {
			t.Fatalf("expected prepared messages to strip images for non-vision model, got images in role=%s", msg.Role)
		}
	}
}
