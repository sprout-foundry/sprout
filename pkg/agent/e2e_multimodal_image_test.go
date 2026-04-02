package agent

import (
	"os"
	"path/filepath"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lastUserMessageInHistory returns a pointer to the last message with role
// "user" in the given message history, or nil if none is found.
func lastUserMessageInHistory(msgs []api.Message) *api.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return &msgs[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test 1: Vision model — image flows through to API as multimodal content
// ---------------------------------------------------------------------------

// TestE2E_MultimodalImage_VisionModel_SendsImageToAPI verifies the full
// end-to-end pipeline for a vision-capable client:
//
//  1. User query contains a pasted-image placeholder.
//  2. processImagesInQuery reads the file and returns []ImageData + cleaned text.
//  3. The user message appended to agent.messages has Images populated.
//  4. The message sent to the model (visible via GetSentRequests) contains
//     the image with Type="image/png" and a valid Base64 payload.
func TestE2E_MultimodalImage_VisionModel_SendsImageToAPI(t *testing.T) {
	t.Parallel()

	// --- Setup: temp directory with pasted image ---
	dir := t.TempDir()

	pasteDir := filepath.Join(dir, ".ledit", "pasted-images")
	require.NoError(t, os.MkdirAll(pasteDir, 0o755))

	imgPath := filepath.Join(pasteDir, "screenshot.png")
	require.NoError(t, os.WriteFile(imgPath, pngMagic, 0o644))

	// --- Build agent with a vision-capable scripted client ---
	// Use workspaceRoot so processImagesAsMultimodal resolves against the temp dir
	// instead of the process CWD (which is shared across parallel tests).
	client := NewScriptedClientWithVision("test-vision-model", stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	agent.workspaceRoot = dir

	query := "Pasted image saved to disk: ./.ledit/pasted-images/screenshot.png — describe this screenshot"

	// --- Execute ---
	result, err := agent.ProcessQuery(query)
	require.NoError(t, err, "ProcessQuery should succeed")
	assert.Equal(t, "Done.", result)

	// --- Assert: user message in agent.messages has images ---
	userMsg := lastUserMessageInHistory(agent.messages)
	require.NotNil(t, userMsg, "expected a user message in agent.messages")
	require.NotEmpty(t, userMsg.Images, "user message should have images attached")
	assert.Equal(t, "image/png", userMsg.Images[0].Type)
	assert.NotEmpty(t, userMsg.Images[0].Base64, "image Base64 should be populated")

	// The cleaned query should contain the [image: filename] tag, not the placeholder.
	assert.Contains(t, userMsg.Content, "[image: screenshot.png]",
		"user message content should contain the cleaned image tag")
	assert.NotContains(t, userMsg.Content, "Pasted image saved to disk:",
		"user message content should not contain the original placeholder text")

	// --- Assert: sent request carries the image payload ---
	sentReqs := client.GetSentRequests()
	require.NotEmpty(t, sentReqs, "expected at least one request sent to the model")

	// Find the user message in the first sent request with images.
	foundImageInSent := false
	for _, msg := range sentReqs[0] {
		if msg.Role == "user" && len(msg.Images) > 0 {
			foundImageInSent = true
			assert.Equal(t, "image/png", msg.Images[0].Type,
				"sent user message image should have type image/png")
			assert.NotEmpty(t, msg.Images[0].Base64,
				"sent user message image should have non-empty Base64")
			break
		}
	}
	assert.True(t, foundImageInSent,
		"the first sent request should contain a user message with images")
}

// ---------------------------------------------------------------------------
// Test 2: Non-vision model — images stripped from prepared messages
// ---------------------------------------------------------------------------

// TestE2E_MultimodalImage_NonVisionModel_StripsImages verifies that when the
// client does NOT support vision:
//
//  1. processImagesInQuery returns nil images (query text unchanged).
//  2. Historical messages that already carry Images are stripped by
//     stripImagesForNonVisionModels inside prepareMessages before sending.
func TestE2E_MultimodalImage_NonVisionModel_StripsImages(t *testing.T) {
	t.Parallel()

	// --- Build agent with a NON-vision scripted client ---
	client := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	agent.workspaceRoot = t.TempDir()

	// Pre-populate agent.messages with a prior turn that includes image data,
	// simulating a conversation where a previous vision-capable model turn
	// already attached images.
	agent.messages = append(agent.messages, api.Message{
		Role:    "user",
		Content: "Previous question with an image",
		Images: []api.ImageData{
			{Type: "image/png", Base64: "iVBORw0KGgoAAAANSUhEUg==", URL: ""},
		},
	})
	agent.messages = append(agent.messages, api.Message{
		Role:    "assistant",
		Content: "Here is my analysis of the image.",
	})

	// --- Execute: a simple text-only query ---
	result, err := agent.ProcessQuery("What is 2+2?")
	require.NoError(t, err, "ProcessQuery should succeed")
	assert.Equal(t, "Done.", result)

	// --- Assert: the historical image should have been stripped from sent messages ---
	sentReqs := client.GetSentRequests()
	require.NotEmpty(t, sentReqs, "expected at least one request sent to the model")

	for _, req := range sentReqs {
		for _, msg := range req {
			assert.Empty(t, msg.Images,
				"non-vision model should never see images in prepared messages (role=%s)", msg.Role)
		}
	}

	// The original agent.messages should still contain the image (only prepared
	// copies are stripped, not the canonical history).
	var historicalUser *api.Message
	for _, m := range agent.messages {
		if m.Role == "user" && m.Content == "Previous question with an image" {
			historicalUser = &m
			break
		}
	}
	require.NotNil(t, historicalUser, "historical user message should still exist")
	assert.NotEmpty(t, historicalUser.Images,
		"historical messages in agent.messages should retain their images (stripping is only for prepared copies)")
}

// ---------------------------------------------------------------------------
// Test 3: Multiple images in a single query through the full pipeline
// ---------------------------------------------------------------------------

// TestE2E_MultimodalImage_MultipleImages processes a query that references
// two pasted images and verifies both are attached to the user message.
func TestE2E_MultimodalImage_MultipleImages(t *testing.T) {
	t.Parallel()

	// --- Setup: temp directory with two pasted images ---
	dir := t.TempDir()

	pasteDir := filepath.Join(dir, ".ledit", "pasted-images")
	require.NoError(t, os.MkdirAll(pasteDir, 0o755))

	img1Path := filepath.Join(pasteDir, "screenshot1.png")
	require.NoError(t, os.WriteFile(img1Path, pngMagic, 0o644))

	img2Path := filepath.Join(pasteDir, "screenshot2.png")
	require.NoError(t, os.WriteFile(img2Path, pngMagic, 0o644))

	// --- Build agent with vision client ---
	client := NewScriptedClientWithVision("test-vision-model", stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	agent.workspaceRoot = dir

	query := "Pasted image saved to disk: ./.ledit/pasted-images/screenshot1.png and " +
		"Pasted image saved to disk: ./.ledit/pasted-images/screenshot2.png — compare these"

	// --- Execute ---
	result, err := agent.ProcessQuery(query)
	require.NoError(t, err, "ProcessQuery should succeed")
	assert.Equal(t, "Done.", result)

	// --- Assert: user message has exactly 2 images ---
	userMsg := lastUserMessageInHistory(agent.messages)
	require.NotNil(t, userMsg, "expected a user message in agent.messages")
	require.Len(t, userMsg.Images, 2, "user message should have exactly 2 images attached")
	assert.Equal(t, "image/png", userMsg.Images[0].Type)
	assert.Equal(t, "image/png", userMsg.Images[1].Type)
	assert.NotEmpty(t, userMsg.Images[0].Base64)
	assert.NotEmpty(t, userMsg.Images[1].Base64)

	// The cleaned query should contain both image tags.
	assert.Contains(t, userMsg.Content, "[image: screenshot1.png]")
	assert.Contains(t, userMsg.Content, "[image: screenshot2.png]")

	// --- Assert: sent request carries both images ---
	sentReqs := client.GetSentRequests()
	require.NotEmpty(t, sentReqs, "expected at least one request sent to the model")

	for _, msg := range sentReqs[0] {
		if msg.Role == "user" {
			assert.Len(t, msg.Images, 2,
				"sent user message should contain 2 images")
			break
		}
	}
}

// ---------------------------------------------------------------------------
// Test 4: Image outside containment directory is skipped
// ---------------------------------------------------------------------------

// TestE2E_MultimodalImage_OutsideContainmentSkipped verifies that images
// referenced outside the .ledit/pasted-images/ containment directory are
// silently skipped — the query still succeeds but no image data is attached.
func TestE2E_MultimodalImage_OutsideContainmentSkipped(t *testing.T) {
	t.Parallel()

	// --- Setup: temp directory with image OUTSIDE containment ---
	dir := t.TempDir()

	// Create the pasted-images directory (empty — no allowed images).
	pasteDir := filepath.Join(dir, ".ledit", "pasted-images")
	require.NoError(t, os.MkdirAll(pasteDir, 0o755))

	// Create an image file OUTSIDE the containment directory.
	outsideDir := filepath.Join(dir, "other-dir")
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))
	outsideImg := filepath.Join(outsideDir, "sneaky.png")
	require.NoError(t, os.WriteFile(outsideImg, pngMagic, 0o644))

	// --- Build agent with vision client ---
	client := NewScriptedClientWithVision("test-vision-model", stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	agent.workspaceRoot = dir

	// Query references an image path outside the containment directory.
	query := "Pasted image saved to disk: ./other-dir/sneaky.png — describe this"

	// --- Execute ---
	result, err := agent.ProcessQuery(query)
	require.NoError(t, err, "ProcessQuery should succeed even when image is outside containment")
	assert.Equal(t, "Done.", result)

	// --- Assert: no images in the user message ---
	userMsg := lastUserMessageInHistory(agent.messages)
	require.NotNil(t, userMsg, "expected a user message in agent.messages")
	assert.Empty(t, userMsg.Images,
		"images outside the containment directory should be skipped (no images attached)")

	// The query text should still have the placeholder replaced (the text rewriting
	// happens before the file-read loop), but no image data.
	assert.Contains(t, userMsg.Content, "[image: sneaky.png]",
		"image tag should appear even if the image was skipped during file read")
}
