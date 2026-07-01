# SP-103 Addendum: Multimodal-Default Era Gaps

_Companion to `TODO.md` SP-103 (lines 128-413 in current main)._
Captures vision-pipeline work that's only visible if you assume most
2026 models are inline-multimodal by default. SP-103-A/B/C were scoped
in mid-2026 when default models were already multimodal; the items here
are the ones that only matter in *that* world.

The original SP-103 framing under-counted this: it described
`analyze_image_content` as the "specialized analysis" path, but in
a multimodal-default era it's increasingly also the **fallback / escape
hatch** when the inline path hits a 429, a vision context-limit, or
an image-too-large wall.

When the runner gets to these items, edit the relevant section in
`TODO.md` SP-103 to fold them in (or leave this addendum as a
follow-on expansion).

## SP-103-D1: Bridge inline-image cost into the budget tracker

Today `lastVisionUsage` / `visionCacheUsage`
(`pkg/agent_tools/vision_types.go:114-116`) only get populated when the
chat flow hits `analyze_image_content` as a tool call. When
`processImagesAsMultimodal` embeds images directly into the user
message, the per-image `image_tokens` /
`cache_read_input_tokens` / `cache_creation_input_tokens` come back in
the provider's chat response but are dropped on the floor before
reaching `BudgetTracker.Deduct`.

Bridge them: add a callback into `pkg/agent/conversation.go`'s
`processImagesAsMultimodal` that registers each embedded image
(sha256 of bytes, dim, mime) and a post-response hook that reads
`usage.output_tokens` / `usage.cache_*_input_tokens` and `Deduct`s
against the budget. **Most users will use more vision spend than they
realize today** — this closes the largest gap between perceived and
actual cost.

**Effort:** ~1.5 days. Touches `pkg/agent/conversation.go`, the
Anthropic + OpenAI providers (`pkg/agent_api/anthropic*.go`,
`openai*.go`) to surface a `MultimodalUsage` field on the chat
response struct, `pkg/budget/budget.go` to accept the new usage
component, and the CLI footer cost reporter.

**Test:** `analyze_image_content` tool path and inline-multimodal path
both contribute to `BudgetTracker.TotalSpent`; verify with a mock chat
response that includes a synthetic `image_tokens: 1500` field.

## SP-103-D2: Batch splitting with fallback

When a user pastes N images and the provider has a vision-context-window
limit lower than `sum(token_cost(image_i))`, the inline path *fails
outright* with a 400 from the provider. The right behavior: try the
inline path; if the provider returns a vision-context overflow (not a
generic 4xx — distinguish via `code: "context_length_exceeded"` or
message substring), automatically split the batch: keep the first K
images inline, call `analyze_image_content` for the rest, and stitch
the results. Same approach for rate-limit-on-vision-chunked-input and
image-too-large errors.

**Effort:** ~1 day. New `pkg/agent_tools/vision_batch_split.go`
dispatch helper. Touches `processImagesAsMultimodal` and adds the
fallback path. Existing A8 graceful-degradation work is the
single-image version of this; D2 is the multi-image version.

## SP-103-D3: Provider-specific vision-capability tables

Right now `SupportsVision()` is a binary per provider, not a richer
capability descriptor. In a multimodal-default world we want to know:
max image bytes, max image count per request, max image dimensions,
native-detail-tier settings (Anthropic: low/high; OpenAI: low/high/auto;
Gemini: static). Add a `VisionCapabilities` struct per provider and
pull into `processImagesAsMultimodal` so resize (B2) and batch
splitting (D2) can be tuned per provider. Read once at construction.
Spec the schema in `pkg/agent_api/interface.go`.

**Effort:** ~0.5 day. Add `VisionCapabilities` types and populate from
model metadata (Anthropic: hard-coded table; OpenAI: derive from
`image_url.detail` accepted values; Ollama local: read from model
manifest).

## Framing changes that should propagate to SP-103 v1

When the runner picks up these items, also make these framing tweaks
to the SP-103 section in `TODO.md`:

1. **SP-103 intro paragraph**: note that "the 2026 default-model
   landscape (Claude Sonnet/Opus 4.5, GPT-5 family, Gemini 2.x,
   Gemma 3, Qwen2.5-VL, Llama 3.2 Vision, MiniMax M2/M3) is
   inline-multimodal as the recommended primary pattern." This sets
   up the D-section naturally.

2. **Background bullets**:
   - `processImagesAsMultimodal` is the default for *every shipped
     configuration* in 2026 — note this is no longer "when
     `SupportsVision() == true`" but "always, except for OCR
     specialists and force-off."
   - `analyze_image_content` is increasingly a fallback / escape
     hatch (in addition to specialized analysis).
   - Add a bullet for "Multimodal-cost instrumentation gap":
     inline-image tokens are dropped on the floor today.

3. **SP-103-C1**: reframe from "dead code reactivation" to "niche
   path (legacy 7B / force-off), but real for those users."

4. **SP-103-C3**: reframe as "describe the escape-hatch behavior
   for users who hit the multimodal path's limits."

5. **Acceptance**: add D1/D2/D3 acceptance bullets (per-image cost
   tracked, batch-split on overflow, capability-table-driven).

## Reference: 2026 multimodal-default model landscape

These are all inline-multimodal-by-default in their respective APIs:

- **Anthropic Claude Sonnet/Opus 4.5**: `image` content blocks as
  first-class; `cache_control` on images (1024-token min, 5-min TTL,
  cache hits cost 10% of base).
- **OpenAI GPT-5 family / GPT-4o**: `image_url` content blocks;
  detail=low/high/auto per image; no native prompt cache on images
  yet (relies on URL stability).
- **Google Gemini 2.x**: `inline_data` parts; native multimodal
  caching.
- **Meta Llama 3.2 Vision**: image tokens in user turn; no built-in
  caching layer.
- **Qwen2.5-VL / Qwen3-VL**: `image` content blocks; dynamic
  resolution control.
- **MiniMax M2/M3**: inline image embedding as primary; vision-capable
  by default across all model tiers.
- **Local Ollama** (`llava`, `llama3.2-vision`, `qwen2.5vl`,
  `minicpm-v`): inline image embedding when multimodal model is
  loaded.

The OCR-specialist carve-out (C2) remains real for local
OCR-only models (`glm-ocr`, `got-ocr2`, `surya-ocr`, etc.) — these
report `SupportsVision() == true` for naive checks but aren't suitable
for conversational inline embedding.