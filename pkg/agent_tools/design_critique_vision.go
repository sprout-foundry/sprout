//go:build !js

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// runCritiqueVisionPass sends the rendered pixels at pngPath and the rubric
// instruction to the vision tier through the SP-137 entry point and returns the
// analysis text. A missing or failing vision tier is not an error: the caller
// degrades to the static rubric result (item 4.2 layers the full degradation on
// top of this).
//
// When the environment carries a wired VisionProcessor (the agent path always
// does — see pkg/agent's ToolEnv construction), the pass goes through it
// directly: it is the same tier AnalyzeImage resolves, but it keeps the call
// hermetic and injectable.
//
// Otherwise the package-level AnalyzeImage is used, which resolves the
// registry-driven vision client (SP-137). That path is guarded by the
// capability probe first: AnalyzeImage would otherwise be free to *construct* a
// vision client and issue a live request, so a run with no vision tier would
// depend on the host's provider configuration and network rather than degrading
// immediately. The probe is local (native-OCR shim, provider configs) and
// touches no network.
//
// A pass that yields no analysis text is reported as an error, not as an empty
// success, so the caller's `visual` decision is always backed by evidence.
func runCritiqueVisionPass(ctx context.Context, env ToolEnv, instruction, pngPath string) (string, error) {
	if strings.TrimSpace(pngPath) == "" {
		return "", errors.New("no rendered image to critique")
	}

	if env.VisionProcessor != nil {
		analysis, err := env.VisionProcessor.AnalyzeImage(ctx, pngPath, instruction)
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(analysis.Description)
		if text == "" {
			return "", errors.New("vision tier returned no critique")
		}
		return text, nil
	}

	if !HasVisionCapability() {
		return "", errors.New("no vision capability available: no wired vision processor and no configured vision provider")
	}

	out, err := AnalyzeImage(ctx, pngPath, instruction, visionModeFrontend)
	if err != nil {
		return "", err
	}
	// AnalyzeImage reports a missing capability as a structured JSON response
	// with an error code rather than a Go error, so unwrap that response. A
	// response that is not a successful analysis is the same "not a critique"
	// signal as an empty one.
	var resp ImageAnalysisResponse
	if jerr := json.Unmarshal([]byte(out), &resp); jerr == nil {
		if !resp.Success {
			msg := resp.ErrorMessage
			if msg == "" {
				msg = resp.ErrorCode
			}
			if msg == "" {
				msg = "vision tier returned no critique"
			}
			return "", errors.New(msg)
		}
		if text := strings.TrimSpace(resp.ExtractedText); text != "" {
			return text, nil
		}
	}
	text := strings.TrimSpace(out)
	if text == "" {
		return "", errors.New("vision tier returned no critique")
	}
	return text, nil
}
