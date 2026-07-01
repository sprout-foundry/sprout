//go:build !js

package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// VisionProgressFunc is a callback invoked after each image's OCR completes
// (success or failure). completed is the number of images processed so far
// (1-indexed), and total is the total number of images in the batch.
type VisionProgressFunc func(completed, total int)

// ---------------------------------------------------------------------------
// Worker pool configuration.
// ---------------------------------------------------------------------------

// getVisionParallelWorkers returns the number of parallel OCR workers.
//
// Reads VISION_PARALLEL_WORKERS from the environment. The value defaults to 3
// when the env var is unset or invalid, and is clamped to the range [1, 32].
func getVisionParallelWorkers() int {
	n := 3 // default
	if raw := configuration.GetEnvSimple("VISION_PARALLEL_WORKERS"); raw != "" {
		if matched, err := fmt.Sscanf(raw, "%d", &n); err == nil && matched == 1 {
			// parsed successfully
		} else {
			n = 3
		}
	}
	if n < 1 {
		n = 1
	}
	if n > 32 {
		n = 32
	}
	return n
}

// ---------------------------------------------------------------------------
// Per-image OCR worker.
// ---------------------------------------------------------------------------

// runOCROne performs OCR on a single image. It encapsulates the loop body
// that was previously inside processOCRImages: optimize → encode → send →
// capture response. The retry behavior is preserved via DoVisionRetry.
//
// Returns the extracted text (may be empty string on failure/skip) and an
// error if the image could not be processed at all (e.g., too large, API
// error). The caller should treat (empty string, nil) as "skipped / no text"
// and (empty string, error) as "failure".
func runOCROne(ctx context.Context, idx int, imgData []byte, client api.ClientInterface, sectionLabel string, prompt string) (string, error) {
	imagePathHint := fmt.Sprintf("pdf_%s_%d.png", strings.ToLower(sectionLabel), idx+1)
	preparedData := imgData
	imgType := detectImageMimeType(imagePathHint)

	optimizedData, optimizedMimeType, optErr := OptimizeImageData(imagePathHint, preparedData)
	if optErr == nil && len(optimizedData) > 0 {
		preparedData = optimizedData
		if optimizedMimeType != "" {
			imgType = optimizedMimeType
		}
	}

	// Skip images that exceed the size cap after optimization.
	if len(preparedData) > visionMaxImageFileSizeBytes {
		return "", fmt.Errorf("image %d exceeds size limit after optimization", idx+1)
	}

	imgBase64 := base64.StdEncoding.EncodeToString(preparedData)
	messages := []api.Message{
		{Role: "user", Content: prompt, Images: []api.ImageData{{Base64: imgBase64, Type: imgType}}},
	}

	var response *api.ChatResponse
	err := DoVisionRetry(ctx, func(ctx context.Context) error {
		var innerErr error
		response, innerErr = client.SendVisionRequest(ctx, messages, nil, "", false)
		return innerErr
	}, RetryOptions{OpName: "ocr_vision"})
	if err != nil {
		return "", fmt.Errorf("OCR vision request failed for image %d: %w", idx+1, err)
	}

	if len(response.Choices) > 0 && response.Choices[0].Message.Content != "" {
		return response.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("OCR returned empty response for image %d", idx+1)
}

// ---------------------------------------------------------------------------
// Parallel OCR entry point.
// ---------------------------------------------------------------------------

// processOCRImagesParallel processes images in parallel using a bounded worker
// pool. It preserves the original ordering of results and supports a progress
// callback.
//
// The worker pool size is determined by getVisionParallelWorkers() (default 3,
// configurable via VISION_PARALLEL_WORKERS env var).
//
// Failures are tracked atomically. When the failure count reaches 2 or more,
// remaining work is cancelled via eg.Cancel(), but already-running goroutines
// are allowed to finish.
//
// Results are joined in index order with section labels between them. If all
// images fail, an error is returned.
func processOCRImagesParallel(ctx context.Context, images [][]byte, client api.ClientInterface, sectionLabel string, progressFn VisionProgressFunc) (string, error) {
	total := len(images)
	if total == 0 {
		return "", fmt.Errorf("OCR failed for all extracted %ss", strings.ToLower(sectionLabel))
	}

	results := make([]string, total)
	var failures atomic.Int32
	var completed atomic.Int32

	workers := getVisionParallelWorkers()

	// Create a cancellable context for the worker pool. We use a plain
	// errgroup (not WithContext) so we control cancellation ourselves:
	// cancel after 2+ failures, not on the first error.
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var eg errgroup.Group
	eg.SetLimit(workers)

	prompt := GetOCRPrompt()

	for i := 0; i < total; i++ {
		idx := i // capture loop variable
		eg.Go(func() error {
			// Note: we do NOT short-circuit on a cancelled context here.
			// The ctx is threaded through to the leaf API call so that
			// callers can observe the cancellation propagating end-to-end.
			// (This preserves the behavior verified by
			// TestProcessOCRImages_CancelledContext.)

			text, err := runOCROne(cancelCtx, idx, images[idx], client, sectionLabel, prompt)
			if err != nil {
				failCount := failures.Add(1)
				results[idx] = "" // explicitly mark as empty

				// If this is the 2nd (or later) failure, cancel remaining work.
				if failCount >= 2 {
					cancel()
				}
			} else {
				results[idx] = text
			}

			// Track completion and invoke progress callback.
			n := completed.Add(1)
			if progressFn != nil {
				progressFn(int(n), total)
			}

			return err
		})
	}

	// Wait for all goroutines (including those already running) to finish.
	_ = eg.Wait()

	// Join results in index order with section labels.
	var allText strings.Builder
	for i := 0; i < total; i++ {
		if results[i] == "" {
			continue
		}
		if allText.Len() > 0 {
			allText.WriteString("\n\n--- ")
			allText.WriteString(sectionLabel)
			allText.WriteString(" ")
			allText.WriteString(fmt.Sprintf("%d", i+1))
			allText.WriteString(" ---\n\n")
		}
		allText.WriteString(results[i])
	}

	if allText.Len() == 0 {
		return "", fmt.Errorf("OCR failed for all extracted %ss", strings.ToLower(sectionLabel))
	}
	return allText.String(), nil
}
