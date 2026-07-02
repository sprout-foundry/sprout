package modelprobe

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// visionImage generates a small solid-red PNG and returns it as base64-encoded ImageData.
func visionImage() (api.ImageData, error) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	red := color.RGBA{255, 0, 0, 255}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, red)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return api.ImageData{}, err
	}

	return api.ImageData{
		Base64: base64.StdEncoding.EncodeToString(buf.Bytes()),
		Type:   "image/png",
	}, nil
}

// runVision sends a single image-bearing message and checks if the model
// correctly identifies the image content. Returns a tierOutcome.
//
// Error classification:
//   - A successful response is scored by whether the model identified the color.
//   - A 4xx error indicating the model can't process images ("does not accept
//     image input", "No endpoints found that support image input", etc.) is a
//     definitive vision=false result — the model has told us it can't see images.
//   - Any other error (5xx, timeout, transport) is propagated as stats.err so
//     Run() marks the whole probe inconclusive.
func runVision(ctx context.Context, client api.ClientInterface) tierOutcome {
	img, err := visionImage()
	if err != nil {
		return tierOutcome{stats: driveStats{err: err}}
	}

	msgs := []api.Message{
		{Role: "system", Content: "You are a vision assistant. Use the describe_image tool to report what you see."},
		{
			Role:    "user",
			Content: "What is the dominant color in this image? Call describe_image with your answer.",
			Images:  []api.ImageData{img},
		},
	}
	tools := []api.Tool{
		fn("describe_image", "Describe what you see in the image.", props("color", "the dominant color of the image"), "color"),
	}

	resp, err := client.SendChatRequest(ctx, msgs, tools, "", false)
	if err != nil {
		if isVisionUnsupportedError(err) {
			return tierOutcome{score: 0, passed: false, reason: "provider rejected image input: " + err.Error()}
		}
		return tierOutcome{stats: driveStats{err: err}}
	}

	st := driveStats{turns: 1, prompt: resp.Usage.PromptTokens, compl: resp.Usage.CompletionTokens}

	if len(resp.Choices) == 0 {
		return tierOutcome{score: 0, passed: false, reason: "no response choices", stats: st}
	}

	msg := resp.Choices[0].Message
	traceTurn("vision", 1, resp, msg)
	if len(msg.ToolCalls) > 0 {
		st.anyTool = true
	}

	a, ok := toolArgs(msg, "describe_image")
	if !ok {
		return tierOutcome{score: 0, passed: false, reason: "did not call describe_image", stats: st}
	}

	color := strings.ToLower(strings.TrimSpace(argString(a, "color")))
	if strings.Contains(color, "red") {
		return tierOutcome{score: 1.0, passed: true, reason: "correctly identified red image", stats: st}
	}

	return tierOutcome{score: 0, passed: false, reason: "wrong color: " + color, stats: st}
}

// isVisionUnsupportedError checks whether an error from the vision request
// indicates the model definitively cannot process images, as opposed to a
// transient transport failure. These are 4xx errors from the provider that
// explicitly reference image/vision/modality support.
func isVisionUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "http 4") {
		return false
	}
	return strings.Contains(msg, "image") ||
		strings.Contains(msg, "vision") ||
		strings.Contains(msg, "multimodal") ||
		strings.Contains(msg, "modality") ||
		strings.Contains(msg, "does not accept")
}
