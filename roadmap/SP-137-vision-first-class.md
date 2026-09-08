# SP-137 — Vision & Image Analysis: First-Class Paths, Provider Neutrality, Native OCR

## Problem

Image analysis is supposed to be a first-class citizen in sprout. It is not.
The failure mode is concrete: during a CLI session a model encountered an
image on disk, tried to inspect it, and — after both `read_file` and
`analyze_image_content` failed it — wrote a throwaway Swift program invoking
the macOS Vision framework to OCR the image itself. The model had multimodal
capability (glm-5.3-flash is natively multimodal); the tool infrastructure
refused to deliver the image to it.

Root causes, verified in code 2026-09-08:

### B1 — Seed registry silently discards tool-result images (blocker)

`core.ToolRegistry.runWithTimeout` (seed v1.3.20, `tool_registry.go:344`)
executes `HandlerWithImages` handlers as `_, text, err := ...` — the image
data is dropped on the floor. `executeSingle` then builds the tool-result
message with `ToolResultMessage(callID, name, text)`, which has no image
parameter. `core.Message` already has an `Images []ImageData` field; it is
simply never populated from tool results.

Consequence: `read_file` on a PDF renders pages to images, faithfully
converts them to `core.ImageData` in `convertHandlerToSeedToolConfig`, and
seed throws them away. The model receives
`[PDF file: x (N pages rendered as images for visual analysis)]` — text
promising images that never arrive. The entire PDF multimodal path is dead
in the seed architecture.

### B2 — `HasVisionCapability()` hardcodes a provider allowlist (blocker)

`pkg/agent_tools/vision_client.go:339` checks a fixed list — DeepInfra,
OpenRouter, OpenAI, Mistral, DeepSeek, `zai`, custom providers, Ollama —
looking for a *separate* vision model behind a *separate* API key.
`zai-coding` (subscription), `lmstudio`, `minimax`, `chutes`, and
`cerebras` are absent. For a zai-coding user with no other keys,
`analyze_image_content` returns:

> vision analysis not available - please set up DEEPINFRA_API_KEY,
> OPENROUTER_API_KEY, or OPENAI_API_KEY

…even though their primary model is natively multimodal. This dead end is
what invited the model to improvise.

### B3 — `read_file` on an image returns raw binary garbage

`read_file` has a PDF branch (`handlePDF`: base64 data-URI + `Images` +
extracted text). There is no image branch. Reading `screenshot.png` dumps
binary bytes into the context window.

### B4 — The multimodal-aware analyze handlers are dead code

`handleAnalyzeImageContentWithImages` and `handleAnalyzePDFWithImages`
(`pkg/agent/tool_handlers_analysis.go`, ~350 lines) route images inline
when the primary model supports vision. Nothing calls them — the active
path is the `pkg/agent_tools` handler gated by B2's broken allowlist.

### B5 — No discoverability guidance

The system prompt never mentions image handling. The "OCR Trigger Policy"
nudge (`buildNonVisionImageToolPrompt`) fires only for pasted images with
non-vision models. A model that meets an image path in the wild — shell
output, `list_dir`, a user saying "look at screenshot.png" — has no signal
that `analyze_image_content` exists or that `read_file`-ing a binary image
is wrong.

### B6 — The vision tier is provider-coupled where it must be neutral

The vision pipeline grew Ollama-shaped parts that should never have been
provider-specific:

- `PDFOCREnabled` / `PDFOCRProvider` / `PDFOCRModel` / `PDFOCRDownloaded`
  config fields (`pkg/configuration/config.go:182-185`) — defaults
  `PDFOCRProvider: "ollama"`, `PDFOCRModel: "glm-ocr"` — encode one
  provider's naming scheme (`EnsureOllamaModelTag`) into global config.
- `GetVisionModelForProvider` special-cases Ollama with a hardcoded
  `"glm-ocr:latest"` default model name (`vision_client.go:217-221`).
- `CreateOllamaClient` / `EnsureOllamaModelTag` exported from the vision
  package — provider plumbing living in the vision tier.
- The OCR-fallback tier (`vision_fallback.go`) resolves to `PDFOCRModel`
  specifically, i.e. "an Ollama model somewhere" is the architecture's
  answer to "what if vision fails".
- WebUI settings patch surface exposes `pdf_ocr_provider`/`pdf_ocr_model`
  raw (`settings_api_partial_settings.go:362`).

**Rule going forward: no vision-tier code may reference a specific provider
by name.** Provider selection belongs to the provider registry
(`vision_model` fields in provider configs); capability discovery belongs
to a provider-neutral resolver. This rule is the point of this spec — the
same class of mistake must not be repeated with the next local runtime.

## Non-goals

- No new vision models, providers, or catalog entries.
- No WebUI vision features (CLI paths only; WebUI can follow).
- Video/animation support.

## Design

### Phase 1 — Unbreak delivery (seed + capability resolution)

**1a. Seed: preserve tool-result images.** In seed's `core`,
`runWithTimeout`/`executeSingle` gain an image-preserving path: when a
`HandlerWithImages` handler returns images, the tool-result `Message`
carries them in `Message.Images`. `ToolResultMessage` grows an images
variant (or a new constructor `ToolResultMessageWithImages`). Sprout's
`convertHandlerToSeedToolConfig` already returns `[]core.ImageData` — no
sprout-side change needed beyond the seed bump. Verify the provider layer
(sprout's `Chat` conversion) maps `Message.Images` on tool results into
the provider request the same way user-message images are mapped
(`attachPastedImages` shows the pattern).

**1b. Provider-neutral capability resolution.** Replace B2's allowlist
with a resolver that answers, in order:

1. *Primary model multimodal?* Ask the active client
   (`SupportsVision()`, honoring the probe cache). If yes, vision is
   available — full stop. No second provider hunt.
2. *Else, any configured provider with a `vision_model`?* Iterate the
   provider registry / custom providers — not a hardcoded list. Local
   runtimes (Ollama, LM Studio, MLX) participate through registry data,
   not special cases.
3. *Else, native OCR (Phase 3)* — text extraction only.

Delete `HasVisionCapability`'s hardcoded provider enumeration.
`GetVisionModelForProvider` shrinks to "read `vision_model` from the
provider's config" with no per-provider defaults baked in.

**1c. `read_file` image branch.** Mirror `handlePDF`:

- Vision-capable primary → return `Images` (data-URI) + short text
  (`[image: name, WxH]`). With 1a, these actually reach the model.
- Non-vision → run the OCR route (Phase 3 native first, then any
  configured remote vision model) and return extracted text.
- Never dump binary. Size/format guard via `console.DetectImageMagic` +
  the existing 10 MB pasted-image cap as the reference budget.

**1d. Wire the multimodal analyze handlers (B4).** Route
`analyze_image_content` through the `WithImages` path when the primary
model supports vision: image goes inline, `analysis_prompt` becomes the
guiding text, no separate vision model involved. Keep the remote-vision
path as fallback for non-vision primaries. Delete any still-unreachable
duplication after wiring.

**1e. System-prompt guidance (B5).** One concise block in both prompts
(`system_prompt.md`, `system_prompt.lite.md`): images and PDFs go through
`analyze_image_content` (modes: `ocr`, `general`); never `read_file` a
binary image; pasted images land in `.sprout/pasted-images/`. Tool
descriptions updated to match.

### Phase 2 — Excise the provider-coupled OCR config (B6)

- Replace `PDFOCRProvider`/`PDFOCRModel`/`PDFOCRDownloaded` with a single
  neutral field: `ocr_fallback_model` — a fully-qualified
  `provider/model` string resolved through the normal provider registry,
  or empty for "native only". `PDFOCREnabled` → `ocr_fallback_enabled`
  (keep the gate; default on).
- Migration: map existing `pdf_ocr_provider`+`pdf_ocr_model` →
  `ocr_fallback_model` (`config_migration.go`); drop
  `EnsureOllamaModelTag` from the vision package — tag etiquette is the
  Ollama client's job, applied when that provider is actually used.
- Remove Ollama special-casing from `GetVisionModelForProvider`; delete
  `CreateOllamaClient` (unused after 1b/2) or move it next to the factory
  where provider plumbing belongs.
- WebUI settings surface updated to the new keys.
- Error text when nothing is available must name the *capability*, not
  providers: "No vision capability available: primary model is
  non-visual, no provider has a vision model configured, and native OCR
  is unavailable on this platform."

### Phase 3 — Native OCR tier

Offline, free, zero-config OCR using the OS text-recognition stack — the
model's ocr.swift improvisation, done properly. Platform shims:

- **macOS**: Vision framework (`VNRecognizeTextRequest`, accurate +
  language correction). A ~30-line Swift helper compiled on demand (or
  bundled per release) into the sprout state dir; `swiftc` present on
  any dev box with Xcode CLT. Fallback: `osascript` JXA invoking Vision.
  Reference implementation (written by a model improvising in the wild —
  the incident that surfaced this spec):

  ```swift
  import Foundation
  import Vision
  import AppKit

  let path = CommandLine.arguments[1]
  guard let img = NSImage(contentsOfFile: path),
        let tiff = img.tiffRepresentation,
        let bmp = NSBitmapImageRep(data: tiff),
        let cg = bmp.cgImage else {
    print("could not load image"); exit(1)
  }
  let request = VNRecognizeTextRequest { req, _ in
    guard let results = req.results as? [VNRecognizedTextObservation] else { return }
    for r in results {
      if let text = r.topCandidates(1).first?.string { print(text) }
    }
  }
  request.recognitionLevel = .accurate
  request.usesLanguageCorrection = true
  let handler = VNImageRequestHandler(cgImage: cg, options: [:])
  try? handler.perform([request])
  ```
- **Windows**: `Windows.Media.Ocr` via PowerShell (built-in).
- **Linux**: `tesseract` when on PATH (document, don't bundle).

A `NativeOCRAvailable()` + `NativeOCR(ctx, path, lang)` seam in
`pkg/agent_tools` with build-tagged platform files, mirroring the
existing `*_unix.go`/`*_windows.go` patterns. Placement in the tier
order: native first for `analysis_mode=ocr` (free, offline); remote
vision model for `general` analysis and when native is absent. Native
failure falls through to remote rather than erroring.

### Acceptance criteria

- [ ] Pasted images still inline for multimodal primaries (regression:
      existing paste tests).
- [ ] `read_file` on `.png`/`.jpg`/`.webp`/`.gif`/`.bmp`/`.avif` returns
      either inline images (multimodal primary) or OCR text — never
      binary.
- [ ] `read_file` on a PDF delivers page images to a multimodal primary
      (seed round-trip test with a scripted client).
- [ ] `analyze_image_content` succeeds for a zai-coding-only user on a
      multimodal primary with **zero** other provider keys.
- [ ] No vision-tier file references any provider by name
      (`grep -rniE 'ollama|deepinfra|openrouter' pkg/agent_tools/vision_*.go`
      outside registry-driven lookups returns nothing).
- [ ] Config carries `ocr_fallback_model` (neutral), old keys migrated.
- [ ] On macOS with no API keys at all, `analysis_mode=ocr` on a local
      screenshot returns its text.
- [ ] System prompt (both variants) mentions the image path; a scripted
      model given an image path and the prompt calls
      `analyze_image_content` (or `read_file`, which now handles it)
      instead of improvising.

## Sequencing & risks

- Phase 1 is the unblocking unit (seed bump required — seed is a sibling
  repo; coordinate the version bump with a `make test-integration` pass
  in `../sprout-foundry` per COMPATIBILITY.md).
- Phase 2 is a breaking config change — ship behind the migration.
- Phase 3's macOS helper has a first-run compile cost; cache the binary
  and gate on `swiftc` presence. Windows/Linux shims are cheap follow-ons.
- The dead code (B4) may reveal more dead weight when wired — budget a
  deletion pass in 1d.
