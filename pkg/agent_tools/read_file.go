package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/injection"
)

// readFileHandler implements ToolHandler for the read_file tool.
type readFileHandler struct{}

func (h *readFileHandler) Name() string {
	return "read_file"
}

func (h *readFileHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "read_file",
		Description: "Read the contents of a file. Text files return lines; images and PDFs are attached inline for visual analysis (never dumped as binary). For large files, use view_range to read specific line ranges. Accepts http(s) URLs for images and PDFs. Set ocr=true to extract text instead of attaching pixels.",
		Parameters: []ParameterDef{
			{
				Name:        "path",
				Type:        "string",
				Required:    true,
				Description: "Path to the file to read, or an http(s) URL for an image or PDF.",
			},
			{
				Name:        "view_range",
				Type:        "array",
				Required:    false,
				Description: "Optional line range as [start, end] array (1-based). Use this to read specific sections of large files. Aliases accepted for convenience: start_line/end_line, line_start/line_end, offset/limit (offset is a 1-based start line, limit a line count). An explicit view_range takes precedence.",
				Items:       map[string]any{"type": "integer"},
			},
			{
				Name:        "ocr",
				Type:        "boolean",
				Required:    false,
				Description: "Extract text from an image or PDF instead of attaching pixels. Use when you need verbatim text (code, tables, dense documents) rather than a visual description.",
			},
		},
		Required: []string{"path"},
	}
}

func (h *readFileHandler) Validate(args map[string]any) error {
	// Fail fast on genuinely unknown arguments: a model that passes
	// "start_line" when only "view_range" exists otherwise gets the
	// whole file back repeatedly with no hint that its argument was
	// ignored. Accepted aliases are translated rather than rejected.
	if err := rejectUnknownReadFileArgs(args); err != nil {
		return err
	}

	path, err := extractString(args, "path")
	if err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("parameter 'path' must not be empty")
	}

	// Validate view_range if provided
	if vr, exists := lookupKey(args, "view_range"); exists && vr != nil {
		arr, ok := vr.([]any)
		if !ok {
			return fmt.Errorf("parameter 'view_range' must be an array")
		}
		if len(arr) != 2 {
			return fmt.Errorf("parameter 'view_range' must have exactly 2 elements: [start, end]")
		}
		for i, v := range arr {
			switch v.(type) {
			case int, float64:
				// Valid numeric types (JSON numbers come as float64)
			default:
				return fmt.Errorf("parameter 'view_range' elements must be integers, got %T at index %d", v, i)
			}
		}
	}

	return nil
}

func (h *readFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	// SP-127 M2: Consult Gate 1's path-tier classifier up-front so
	// Allow paths skip the gate entirely and Deny paths return a
	// typed error immediately — without waiting for SafeResolvePath
	// to fail first. Prompt paths fall through to the interactive
	// dialog.

	// Reject arguments the handler cannot resolve before doing any work.
	// This is the path a model hits when it invents an argument name: a
	// clear error naming the valid ones beats silently reading the whole
	// file again.
	if err := rejectUnknownReadFileArgs(args); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	path, err := extractStringAny(args, append([]string{"path"}, readFilePathAliases...)...)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// Remote images and PDFs: fetched over the network, so they skip the
	// path-tier classifier (there is no local path to classify) and go
	// straight to the content handlers.
	if isHTTPURL(path) {
		return h.handleRemote(ctx, env, path, ocrRequested(args))
	}

	// Gate 1 precheck — resolves the path and classifies it.
	resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "read_file", path)
	if decision == "deny" {
		// A deny on read_file is not a read_only violation — reads are
		// always allowed under read_only grants. Surface a neutral message.
		return ToolResult{Output: fmt.Sprintf("read blocked: %s is not accessible from this session", path), IsError: true},
			fmt.Errorf("read blocked: %s is not accessible", path)
	}
	// "allow"  → path is workspace/tmp/allowlisted; proceed directly.
	// "prompt" → interactive approval; on deny fall through to the raw
	// error. This is the pinned contract (file_access_prompt_test.go):
	// denied and nil-prompter reads surface the underlying fs error, not
	// a synthesized denial — the off-project-folder block still holds because
	// the bypass ctx is only set on approval.
	if decision == "prompt" {
		if ctx2, approved := promptForOffProjectFolderPath(ctx, env, "read_file", path, resolvedPath, "read"); approved {
			ctx = ctx2
		}
	}

	// Resolve the effective line range, honouring the documented aliases
	// (an explicit view_range wins). This must happen before validation of
	// the read so a `start_line`/`end_line` call reaches the same code path
	// as `view_range`.
	startLine, endLine := effectiveReadRange(args)

	// SP-046-2: Record the read for staleness tracking (all code paths, including PDF)
	// Use a defer so this runs regardless of which branch handles the file.
	if tracker := GetGlobalTurnReadTracker(); tracker != nil {
		meta, _ := GetGlobalSyncState().GetMetadata(path)
		seq := int64(0)
		if meta != nil {
			seq = meta.BrowserSeq
		}
		tracker.RecordRead(path, seq)
	}

	// Check if this is a PDF file
	if strings.ToLower(filepath.Ext(path)) == ".pdf" {
		return h.handlePDF(ctx, env, path, ocrRequested(args))
	}

	// Image files: never dump binary into the context (SP-137). Vision-
	// capable models get the image inline as multimodal content; others
	// get OCR-extracted text.
	if isImageExtension(path) {
		return h.handleImage(ctx, env, path, ocrRequested(args))
	}

	// Use existing read logic. Off-project-folder paths will fail
	// with the raw filesystem error since the interactive gate is gone.
	var content string
	if startLine > 0 || endLine > 0 {
		content, err = ReadFileWithRange(ctx, path, startLine, endLine)
	} else {
		content, err = ReadFile(ctx, path)
	}

	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("read file %q: %w", path, err)
	}

	// SP-169 layer 5 (phasing step 3): content we did not write is data, not
	// instructions. The class is decided once per read, from a structural
	// marker or a recorded decision, and only the ambiguous case (a path
	// outside our tree, or a nested clone) asks the user — once, remembered
	// through the trust store. Framing is a label, not a restriction: the
	// content is delivered in full inside the block. Image and binary reads
	// are handled above and are never framed.
	trustPath := trustPathForRead(env, path, resolvedPath)
	if resolveReadTrust(env, trustPath) == injection.TrustThirdParty {
		// The label carries the origin so the model can tell a vendored file
		// from one of ours, which is the point of framing at ingress.
		content = injection.WrapUntrusted("file: "+relToProjectFolder(env.ProjectFolder, trustPath)+" (third-party)", content)
	}

	// Repeat guard: once the read succeeded, an identical call (same path +
	// same effective range) made a third time this turn returns a short note
	// instead of the content again. The read still runs so the file's
	// existence and range validation are unchanged for the first two calls
	// and for error cases; only the (expensive) content payload is withheld.
	// The per-turn counter is owned by the agent (ToolEnv.ReadCallGuard).
	if env.ReadCallGuard != nil {
		if count := env.ReadCallGuard.ObserveRead(path, startLine, endLine); count >= 3 {
			note := readRepeatNote(path, startLine, endLine, count)
			if env.OutputWriter != nil {
				_, _ = io.WriteString(env.OutputWriter, note)
			}
			return ToolResult{
				Output:     note,
				TokenUsage: int64(estimateTokenUsage(note)),
			}, nil
		}
	}

	// Write to output writer if available
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, content)
	}

	return ToolResult{
		Output:     content,
		TokenUsage: int64(estimateTokenUsage(content)),
	}, nil
}

func (h *readFileHandler) Aliases() []string      { return nil }
func (h *readFileHandler) Timeout() time.Duration { return 0 }
func (h *readFileHandler) MaxResultSize() int     { return 0 }
func (h *readFileHandler) SafeForParallel() bool  { return false }
func (h *readFileHandler) Interactive() bool      { return false }

// handlePDF processes a PDF file and returns it as base64 data URI for vision-capable models.
func (h *readFileHandler) handlePDF(ctx context.Context, env ToolEnv, path string, ocr bool) (ToolResult, error) {
	// Resolve path securely. Off-project-folder paths will fail with the raw
	// filesystem error since the interactive gate is gone.
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, path)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("resolve PDF path: %w", err)
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("access PDF file: %w", err)
	}
	if info.IsDir() {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("path is a directory, not a file: %s", cleanPath)
	}
	if info.Size() > inlinePDFMaxBytes {
		return ToolResult{
			Output:  fmt.Sprintf("[PDF %s is %d bytes, over the %d MB inline cap — extract pages or analyze via a vision model]", filepath.Base(path), info.Size(), inlinePDFMaxBytes/1024/1024),
			IsError: true,
		}, fmt.Errorf("PDF too large to read inline: %s", cleanPath)
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("read PDF file: %w", err)
	}

	// Build data URI
	mimeType := mime.TypeByExtension(".pdf")
	if mimeType == "" {
		mimeType = "application/pdf"
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)

	// ocr=true asks for text, not pixels: return the extracted text and skip
	// the data URI, so a caller that wants verbatim content does not pay for
	// an attachment it will not use.
	if ocr {
		text, ocrErr := ProcessPDFForTextOnly(ctx, cleanPath, envProviderType(env))
		if ocrErr != nil {
			return ToolResult{
				Output:  fmt.Sprintf("[PDF %s — text extraction failed: %v]", filepath.Base(path), ocrErr),
				IsError: true,
			}, ocrErr
		}
		out := fmt.Sprintf("[PDF content extracted from %s]\n\n%s", cleanPath, text)
		return ToolResult{
			Output:     out,
			TokenUsage: int64(estimateTokenUsage(out)),
		}, nil
	}

	// Try to extract text via the existing PDF pipeline as supplementary content
	var textContent string
	result, pipelineErr := ProcessPDFForMultimodal(ctx, cleanPath)
	if pipelineErr == nil && result != nil && result.Text != "" {
		textContent = fmt.Sprintf("[PDF content extracted from %s]\n\n%s", cleanPath, result.Text)
	} else if pipelineErr == nil && result != nil && len(result.Images) > 0 {
		textContent = fmt.Sprintf("[PDF file: %s (%d pages rendered as images for visual analysis)]", cleanPath, len(result.Images))
	} else {
		textContent = fmt.Sprintf("[PDF file: %s (%d bytes). Text extraction unavailable: %v. Base64 data URI provided for vision-capable models.]", cleanPath, len(data), pipelineErr)
	}

	return ToolResult{
		Output:     textContent,
		Images:     []ImageData{{URI: dataURI, MIMEType: mimeType}},
		TokenUsage: int64(estimateTokenUsage(textContent)),
	}, nil
}

// imageExtensions are the formats the paste pipeline recognizes
// (console.DetectImageMagic). Keep in sync with it.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".avif": "image/avif",
}

// isImageExtension reports whether path has a known image extension.
func isImageExtension(path string) bool {
	_, ok := imageExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

// maxInlineImageBytes caps read_file's inline image read (10 MB, matching
// the pasted-image budget). Files in the 1-10 MB range are run through the
// optimization cascade (prepareInlineAttachmentPayload) before attaching.
const maxInlineImageBytes = 10 * 1024 * 1024

// inlinePDFMaxBytes caps read_file's inline PDF read. PDFs are base64'd
// verbatim (no compression path), so the cap keeps the encoded body under
// a conservative 10 MB server body limit (5 MB -> ~6.7 MB base64).
const inlinePDFMaxBytes = 5 * 1024 * 1024

// handleImage serves an image file without dumping binary into the model
// context. Vision-capable primary models receive the image inline (the
// seed registry attaches ToolResult.Images to the tool message, and seed's
// prepareMessages strips them for non-vision models). Non-vision models
// get OCR-extracted text via the analyze pipeline.
func (h *readFileHandler) handleImage(ctx context.Context, env ToolEnv, path string, ocr bool) (ToolResult, error) {
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, path)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("resolve image path: %w", err)
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("access image file: %w", err)
	}
	if info.IsDir() {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("path is a directory, not a file: %s", cleanPath)
	}
	if info.Size() > maxInlineImageBytes {
		return ToolResult{
			Output:  fmt.Sprintf("[image file %s is %d bytes, over the %d MB inline cap; re-encode it smaller]", filepath.Base(path), info.Size(), maxInlineImageBytes/1024/1024),
			IsError: true,
		}, fmt.Errorf("image too large to read inline: %s", path)
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("read image file: %w", err)
	}

	// Magic-byte verification is authoritative: an image extension on
	// non-image bytes is an error (never serve garbage as base64).
	_, mimeType := console.DetectImageMagic(data)
	if mimeType == "" {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("unrecognized image format: %s", path)
	}

	// The primary's pixel acceptance decides the wire: for a vision
	// primary the payload is compressed (dimension + JPEG quality cascade)
	// so a bulk screenshot turn cannot grow an HTTP body past the
	// provider's body limit (413). A non-vision primary never sees the
	// pixels — seed strips Images per model — so attach raw (no wire
	// cost) and let the OCR text below carry the analysis.
	primarySees := env.PrimaryAcceptsImages == nil || env.PrimaryAcceptsImages()

	// ocr=true asks for text, not pixels: run the OCR shim and return its
	// output without attaching the image, so a caller that wants verbatim
	// text (code, tables, dense documents) does not pay for pixels.
	if ocr {
		if !nativeOCRAvailable() {
			return ToolResult{
				Output:  fmt.Sprintf("[image: %s — OCR is unavailable on this platform]", filepath.Base(path)),
				IsError: true,
			}, fmt.Errorf("OCR unavailable for %s", path)
		}
		text, ocrErr := nativeOCR(ctx, cleanPath)
		if ocrErr != nil {
			return ToolResult{
				Output:  fmt.Sprintf("[image: %s — OCR failed: %v]", filepath.Base(path), ocrErr),
				IsError: true,
			}, ocrErr
		}
		trimmed, truncated, _ := limitVisionOutputText(strings.TrimSpace(text))
		if truncated {
			trimmed += "\n[truncated]"
		}
		out := fmt.Sprintf("[image: %s (%d bytes, %s)] OCR text:\n%s", filepath.Base(path), info.Size(), mimeType, trimmed)
		return ToolResult{
			Output:     out,
			TokenUsage: int64(estimateTokenUsage(out)),
		}, nil
	}

	var payload []byte
	var payloadMime string
	if primarySees {
		var prepErr error
		payload, payloadMime, prepErr = prepareInlineAttachmentPayload(cleanPath, data, mimeType)
		if prepErr != nil {
			return ToolResult{
				Output:  fmt.Sprintf("[image %s: %v — no pixels attached; re-encode it smaller or analyze via a vision model]", filepath.Base(path), prepErr),
				IsError: true,
			}, prepErr
		}
	} else {
		payload, payloadMime = data, mimeType
	}

	// Vision-capable primary: pixels are the analysis — no OCR text needed
	// (seed strips Images for non-vision models, which get OCR below).
	// Native OCR runs only when the primary cannot see, keeping it a
	// fallback rather than a default.
	var textContent string
	images := []ImageData{{
		URI:      fmt.Sprintf("data:%s;base64,%s", payloadMime, base64.StdEncoding.EncodeToString(payload)),
		MIMEType: payloadMime,
	}}

	if !primarySees && nativeOCRAvailable() {
		if ocrText, ocrErr := nativeOCR(ctx, cleanPath); ocrErr == nil {
			trimmed, truncated, _ := limitVisionOutputText(strings.TrimSpace(ocrText))
			if truncated {
				trimmed += "\n[truncated]"
			}
			if trimmed != "" {
				textContent = fmt.Sprintf("[image: %s (%d bytes, %s)] OCR text:\n%s",
					filepath.Base(path), info.Size(), mimeType, trimmed)
			}
		}
	}
	if textContent == "" {
		if primarySees {
			textContent = fmt.Sprintf("[image: %s (%d bytes, %s) attached — describe what you see]",
				filepath.Base(path), info.Size(), mimeType)
		} else {
			textContent = fmt.Sprintf("[image: %s (%d bytes, %s) attached for visual analysis; OCR text unavailable]",
				filepath.Base(path), info.Size(), mimeType)
		}
	}

	return ToolResult{
		Output:     textContent,
		Images:     images,
		TokenUsage: int64(estimateTokenUsage(textContent)),
	}, nil
}

// ocrRequested reports whether the caller asked for text extraction rather
// than pixels.
func ocrRequested(args map[string]any) bool {
	v, ok := args["ocr"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// handleRemote fetches an http(s) image or PDF and returns it the same way a
// local file would be returned: pixels inline for a vision-capable primary,
// OCR text otherwise. Remote content is never written to the project folder.
func (h *readFileHandler) handleRemote(ctx context.Context, env ToolEnv, url string, ocr bool) (ToolResult, error) {
	primarySees := env.PrimaryAcceptsImages == nil || env.PrimaryAcceptsImages()

	kind, _, ok := SniffURLContent(ctx, url)
	if !ok {
		return ToolResult{
			Output:  fmt.Sprintf("[remote %s: not an image or PDF]", url),
			IsError: true,
		}, fmt.Errorf("remote content is not an image or PDF: %s", url)
	}

	result, err := FetchBinaryURL(ctx, url, kind)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("[remote %s: %v]", url, err),
			IsError: true,
		}, err
	}

	// A text-bearing result (a text-based PDF) carries no pixels.
	if len(result.Images) == 0 {
		if strings.TrimSpace(result.Text) == "" {
			return ToolResult{
				Output:  fmt.Sprintf("[remote %s: no readable content]", url),
				IsError: true,
			}, fmt.Errorf("remote content yielded no text or images: %s", url)
		}
		return ToolResult{
			Output:     result.Text,
			TokenUsage: int64(estimateTokenUsage(result.Text)),
		}, nil
	}

	images := make([]ImageData, 0, len(result.Images))
	for _, img := range result.Images {
		images = append(images, ImageData{
			URI:      fmt.Sprintf("data:%s;base64,%s", img.Type, img.Base64),
			MIMEType: img.Type,
		})
	}

	// The primary cannot see: describe the image with a vision model rather
	// than handing it pixels seed will strip.
	if ocr || !primarySees {
		text := describeRemoteImage(ctx, env, url, images)
		if text != "" {
			return ToolResult{
				Output:     text,
				TokenUsage: int64(estimateTokenUsage(text)),
			}, nil
		}
		// An explicit ocr request must not silently degrade to pixels: the
		// caller asked for text, and on a non-vision primary the attached
		// pixels would be stripped, leaving the model with nothing at all.
		// Mirror the local-image branch's contract.
		if ocr {
			return ToolResult{
				Output:  fmt.Sprintf("[remote image: %s — OCR text unavailable; no vision model could describe it]", url),
				IsError: true,
			}, fmt.Errorf("no OCR text available for remote image %s", url)
		}
	}

	note := fmt.Sprintf("[remote image: %s — attached, describe what you see]", url)
	if result.Text != "" {
		note = result.Text
	}
	return ToolResult{
		Output:     note,
		Images:     images,
		TokenUsage: int64(estimateTokenUsage(note)),
	}, nil
}

// describeRemoteImage runs a non-vision primary's remote image through the
// agent's vision model, returning a provenance-labeled description. Empty
// when no vision client is available or the analysis fails — the caller then
// either attaches pixels (a vision primary) or reports the failure.
//
// The bytes are passed through rather than the URL, so the image is not
// downloaded a second time.
func describeRemoteImage(ctx context.Context, env ToolEnv, url string, images []ImageData) string {
	if env.VisionProcessor == nil || len(images) == 0 {
		return ""
	}
	data, mimeType, ok := decodeDataURI(images[0].URI)
	if !ok {
		return ""
	}
	analysis, err := env.VisionProcessor.AnalyzeImageBytes(ctx, url, data, mimeType, GetStructuredDescriptionPrompt())
	if err != nil || strings.TrimSpace(analysis.Description) == "" {
		return ""
	}
	return fmt.Sprintf("[remote image: %s — described via %s]\n%s", url, env.VisionProcessor.providerLabel(), analysis.Description)
}

// decodeDataURI splits a "data:<mime>;base64,<payload>" URI into its parts.
func decodeDataURI(uri string) ([]byte, string, bool) {
	const prefix = "data:"
	if !strings.HasPrefix(uri, prefix) {
		return nil, "", false
	}
	rest := uri[len(prefix):]
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return nil, "", false
	}
	meta := rest[:comma]
	if !strings.HasSuffix(meta, ";base64") {
		return nil, "", false
	}
	mimeType := strings.TrimSuffix(meta, ";base64")
	data, err := base64.StdEncoding.DecodeString(rest[comma+1:])
	if err != nil {
		return nil, "", false
	}
	return data, mimeType, true
}

// toIntArg converts an interface{} to int, handling float64 from JSON.
func toIntArg(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
