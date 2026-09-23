# SP-140 — Vision as a First-Class Input: Unified Capability Resolution, Optimistic Inline, and Structured-Description Delegation

> **Status:** ✅ Phases 1–4 core landed (2026-09-18); per-model
> `vision_limits` landed 2026-09-22 (`model_info.vision_limits` in
> `pkg/agent_providers`, layered in `GenericProvider.VisionCapabilities`;
> catalog capabilities projection was already in place via
> `cmd/refresh_provider_catalog`'s `CapabilitiesFromTags` merge).
> Remaining: designer-flow batch refinements (deferred to designer spec). Supersedes
> the *capability-gate* posture of SP-137 (whose delivery fixes — seed
> tool-result images, `read_file` image branch, native OCR tier,
> provider-neutral config — remain in force and are built upon here).
>
> **Phase 3 landing notes:** `tools.DelegateImageDescriptions` is the
> delegation rung on the inline chat path — one structured-description prompt
> (`GetStructuredDescriptionPrompt`: type, layout, verbatim text regions,
> hex colors, components, typography) executed through the analyze pipeline
> (`VisionProcessor.AnalyzeImage`, reusing retry + OCR fallback). The
> non-vision paste path now delegates automatically and labels every image
> with provenance (`[image N of M: name — described via provider/model]`);
> the analyze-tool prompt survives only as the fallback when no vision
> client exists at all. `tools.SniffURLContent` closes the URL gap:
> unknown-content-type URLs (S3 signed, CDN, extension-less) get a
> size-capped ranged GET with magic-byte classification before falling back
> to the text handler. WASM degrades explicitly: both are stubbed to
> unavailable, matching the existing WASM vision-tool stubs.
>
> **Phase 4 landing notes:** system prompts (both variants) flipped to
> assumed-vision posture — inline arrival is the default, bracketed
> provenance means degraded seeing, re-run `analyze_image_content` for more
> fidelity. The dead duplicate vision branch in `processImagesInQuery` was
> deleted (both branches re-checked the same resolver answer). WebUI parity
> was already structural: the browser upload surface saves through the same
> `SavePastedImage` + `Pasted image saved to disk:` placeholder, so CLI
> delegation/inline paths apply unchanged; `test/webui/vision-parity.spec.ts`
> covers upload accept/reject e2e (Chrome-channel launch per AGENTS.md).
>
> **Phase 2 landing notes:** `pkg/agent_api/vision_learned.go` adds the
> runtime layer: `RecordVisionAcceptance`/`LearnedVisionAcceptance` persist
> per provider-model observations to a hand-editable
> `<state-dir>/vision-capabilities.json` (positive TTL 30d, negative 7d), and
> `IsVisionCapabilityRejection` conservatively classifies capability-shaped
> 4xx errors (keyword match on the body; 5xx/auth/network never learn).
> `GenericProvider.reconcileVisionCapability` wraps both chat paths:
> image-bearing successes record known-true; capability rejections record
> known-false and retry once text-only with images stripped and a bracketed
> note, so the turn completes instead of erroring (the full delegation
> reroute lands with Phase 3). The resolver's top precedence is now
> runtime-learned > probe > declared, and the paste/read_file/fetch_url
> attach gates route through the resolver, so a model the runtime has seen
> accept images gets them even when config says otherwise — the silent-drop
> failure mode is gone.
>
> **Phase 1 landing notes:** `api.ResolveVisionCapability` shipped with
> probe→declared precedence; `SupportsConversationalVision` deleted from both
> provider interfaces and all implementations; `effectiveVisionSupport`/
> `effectiveConversationalVision` and the agent's private probe cache are
> gone; the inline batch splitter merged into
> `pkg/agent_tools/vision_batch_split.go`. The conversational-vision
> distinction itself (SP-103-C2, incl. the Ollama-local OCR-only special
> case) was then removed entirely: OCR-only models are declared
> vision-capable, inline delivery and the OCR path both yield extracted
> text for them, so the distinction only rerouted delivery — the single
> `AcceptsImages` field carries all callers now. Net −870 LoC (gate holds).
> The `supports_vision` silent-optimism rule (config rule 3) stays in
> `GenericProvider.SupportsVision()` until Phase 2's optimistic-verify lands —
> deleting it first would regress unknown-model vision in the interim window.

## Problem

Most hosted models are now multimodal, and the upcoming designer feature
(screenshot/mockup-driven design work in the WebUI) makes image input a
primary path, not an occasional one. Sprout's vision architecture is still
shaped for the era when vision was the exception:

1. **Three capability sources disagree.** `GenericProvider.SupportsVision()`
   (`pkg/agent_providers/generic_provider_vision.go`) trusts config tags, with
   a blanket-optimism rule (unlisted model + provider-level
   `supports_vision: true` → treated as vision-capable). The published probe
   registry overrides config via `Agent.effectiveVisionSupport()`
   (`pkg/agent/agent_vision_probe.go`). The live OpenAI-compat listing adapter
   (`pkg/modelcontract/openai_compat.go`) emits no vision capability at all.
   Which answer wins depends on which call site you hit.

2. **Capability is configured, never learned.** The probe layer is read-only,
   published offline by the registry pipeline. A provider whose catalog lags
   reality (e.g. `glm-5.3-flash` is vision-tagged in
   `pkg/agent_providers/configs/zai-coding.json` but absent from
   `pkg/providercatalog/providers.json`) ships stale answers until someone
   refreshes a catalog by hand. Nothing at runtime ever records "this model
   accepted/rejected images".

3. **The degradation ladder ends in OCR.** Today: inline on a vision primary →
   (analyze tools only) remote vision model → OCR fallback
   (`ocr_fallback_model`) → native OS OCR. OCR text cannot carry layout,
   palette, spacing, or component hierarchy — the exact content a designer
   feature needs. There is no "delegate this image to the best available
   vision model for a *structured description*" rung on the inline chat path,
   only inside the `analyze_image_content` tools.

4. **Vision limits are provider-level.** `VisionCapabilities` (max image
   bytes/count/dimensions) resolve per provider, not per model. `glm-4.5v`
   (16K context) and `glm-5v-turbo` (200K) on the same provider share batch
   limits, so `BatchSplit` either over-splits the big model or over-sends to
   the small one.

5. **WebUI is a second-class vision citizen.** SP-137 was explicitly CLI-only.
   The designer feature is WebUI-centric; paste/render/inline paths and
   transcript rendering there still route through the CLI-era assumptions.

6. **Parallel implementations of the same job.** Two batch splitters
   (`pkg/agent/vision_batch_split.go` for the inline path vs
   `pkg/agent_tools/vision_batch.go` for the analyze path); two capability
   reads on the chat path (`conversation.go`'s `supportsConversationalVision`
   helper alongside `effectiveVisionSupport`); and
   `SupportsConversationalVision` boilerplate re-implemented in every
   provider client and test fake. A vision rework that adds another read or
   splitter deepens the problem it claims to fix.

**Rule carried forward from SP-137:** no vision-tier code references a
specific provider by name. Capability discovery is registry/config-driven;
selection lives in provider configs.

## Non-goals

- Video/animation input.
- New providers, models, or catalog entries.
- Changing seed's tool-result image preservation (v1.3.21 contract stays).
- The designer feature's own UX (tracked separately; this spec only builds
  the vision substrate it consumes).

## Simplification rules

This spec must leave the vision tier *smaller* than it found it. Binding,
review-checked:

1. **One resolver, one cache, one ladder.** Capability knowledge gets exactly
   one source of truth (the resolver); degradation gets exactly one
   implementation (the existing `pkg/agent_tools` analyze-path machinery).
   Inline-path delegation *calls* it; it is never re-implemented.
2. **Deletions are part of each phase.** Phase 1 deletes the three private
   capability reads (`GenericProvider.SupportsVision` tag logic,
   `effectiveVisionSupport`'s probe cache, `conversation.go`'s
   `supportsConversationalVision` helper), the
   `SupportsConversationalVision` interface method and its per-client /
   per-test-fake re-implementations, and merges the inline batch splitter
   into the analyze path's. Later phases add nothing without their listed
   deletions.
3. **No new config surface.** No override maps, no new env vars. The runtime
   cache file is plain JSON in the state dir and hand-editable — that *is*
   the override mechanism. The only config addition is optional per-model
   `vision_limits` (three ints) in `model_info`.
4. **No new packages or files where existing ones fit.** Resolver lives in
   `pkg/agent_api` (capability types already there); learning cache and
   delegation glue live inside existing vision files.
5. **Net-LoC gate.** The vision tier (`pkg/agent_tools/vision_*`,
   `pkg/agent` vision files, `pkg/agent_api` vision types) ends each phase
   with fewer lines than it started. A phase that adds without deleting
   does not ship.

## Design

### Phase 1 — One capability resolver, per-model limits

New seam: `pkg/agent_api/vision_resolver.go`.

```go
type VisionConfidence int // ConfidenceUnknown, ConfidenceLearned, ConfidenceProbed

type VisionCapability struct {
    Conversational bool              // accepts image parts in chat turns
    Limits         VisionCapabilities // per-model, not per-provider
    Confidence     VisionConfidence
    Source         string            // "runtime" | "probe" | "config" | "default"
    VerifiedAt     string            // RFC3339, when learned/probed
}

func ResolveVisionCapability(ctx context.Context, provider string, model string) VisionCapability
```

Precedence, highest first:

1. **Runtime-learned** (Phase 2 cache) — authoritative until TTL expiry.
2. **Published probe** (`ProbeResult.Vision`, existing registry data).
3. **Config tags** (`model_info` `"vision"` tag via
   `CapabilitiesFromTags`).
4. **Provider default** — `supports_vision` provider flag, but only as the
   *optimistic-unknown* posture of Phase 2, never as a final answer.

No conversational-vision refinement: SP-140 removed the SP-103-C2
distinction (and the per-client heuristics it required, e.g. Ollama-local
OCR-only model-name matching). Clients declare image acceptance; delivery
is the send path's concern, not a per-client property.

Migration — each call site is *deleted*, not wrapped (the resolver owns the
logic; nothing keeps a private copy):

- `GenericProvider.SupportsVision()` tag/flag resolution → resolver call.
- `Agent.effectiveVisionSupport()` + its private probe cache → resolver
  (the resolver owns all caching).
- `conversation.go` `supportsConversationalVision` helper → resolver.
- `seed_provider.go` `attachPastedImages` vision gate (`sp.currentClient().SupportsVision()`)
  → resolver posture. Silent-drop of pasted images on unknown-capability
  models is the worst failure mode for the designer flow; the gate must
  become optimistic-unknown (attach, verify per Phase 2), never a quiet
  `return messages`.
- `Provider.SupportsConversationalVision()` interface method → removed from
  `api.Provider` and from every client and test fake. The real semantic
  behind it (Ollama-local OCR-only models must not receive inline chat
  images) becomes resolver *input data* recorded once, not a method
  re-implemented per client.
- `pkg/agent/vision_batch_split.go` → merged into
  `pkg/agent_tools/vision_batch.go`; both paths share one splitter.

**Delete the silent-optimism rule** (config rule 3: unlisted model +
`supports_vision: true` → true). Unknown becomes a real tri-state the
resolver reports honestly; optimism moves to the send path (Phase 2) where it
is verified and remembered.

**Per-model limits:** `model_info` entries gain three optional ints —
`vision_limits {max_image_bytes, max_image_count, max_dimension}` —
defaulting to the provider config when absent; most models need no entry.
`VisionCapabilities` resolution: model override → provider config →
`VisionCapabilitiesDefault()`. `BatchSplit` consumes the resolved per-model
value.

**Catalog freshness:** `cmd/refresh_provider_catalog` already projects
canonical models; make it emit `Capabilities` from the tags it already reads
(a pure projection). No drift enforcement — Phase 2's runtime learning makes
catalog freshness non-load-bearing.

### Phase 2 — Optimistic inline with verify-and-remember

For any model whose resolver answer is `ConfidenceUnknown`, treat it as
vision-capable at send time and verify against reality:

1. **Send inline.** Image parts go out on the primary model as today.
2. **Classify rejection.** New error classifier in the provider layer
   distinguishes:
   - *capability rejection* — 400-class body mentioning modalities/images
     (`image`, `modalit`, `vision`, `not supported` on the content part):
     record known-false;
   - *size rejection* (413 / token-overflow on image parts): not a capability
     signal — route through `BatchSplit` with tightened limits and retry
     inline;
   - *transient* (429/5xx/timeout): no learning, normal retry.
3. **Remember.** Runtime cache persisted under the SP-133 state dir
   (`vision-capabilities.json`), keyed `provider/model`, entries
   `{conversational bool, verified_at, source}` with a TTL (default 30d) and
   a negative-TTL shorter than positive (default 7d — providers add vision
   to models over time). The resolver reads this as its top-precedence layer.
4. **Reroute, don't fail.** On capability rejection, the in-flight turn
   retries the affected messages through the Phase 3 delegation ladder so the
   user's request completes; the model-visible content degrades to
   attributed descriptions instead of erroring.

Effect: every catalog-lagging provider self-corrects on first contact;
conversations never hard-fail on a capability guess.

### Phase 3 — Delegation as a first-class tier (structured description)

Reframe the fallback ladder as **vision delegation**, with OCR as the
terminal text-extraction rung only:

1. **Inline on the primary** (Phase 2 posture).
2. **Delegate**: registry-driven best available vision model — the machinery
   in `vision_client.go` (`visionProviderCandidates`, `vision_model`
   configs) already exists for the analyze tools — now also invoked from the
   *inline chat path* when the primary cannot see. The delegation prompt is
   a new structured-description mode in `vision_prompts.go`: layout
   structure, spatial relationships, text regions + content, color palette,
   typography hints, component affordances — output as compact structured
   text, not prose.
3. **Native OCR** (`native_ocr_*.go`) for `analysis_mode=ocr` and as the
   final text-only rung. Unchanged.

Requirements:

- **Reuse mandate:** delegation on the inline path *is* the analyze path's
  existing pipeline (`VisionProcessor` + the `vision_client.go` candidate
  walk) invoked with the new structured prompt. No second delegation
  implementation. The subscription-before-pay-per-token preference is one
  sort of the existing candidate list, not a selection subsystem.
- **Provenance labels** on every degraded image, extending the existing
  numbered-label pattern (`conversation_image_label_test.go`):
  `[image 1: attached]` / `[image 2: described via <provider/model>]` /
  `[image 3: OCR text]` — so the model and user know what quality of seeing
  they got. Delegated text must be visually distinct in transcripts (Phase 4).
- **Cost controls:** reuse prompt-cached image blocks
  (`buildMultiModalContent` `cache_control` ephemeral) and
  `vision_cache.go` for repeated screenshots; delegation counts against the
  existing vision usage metrics.
- **Subscription-aware selection:** when several providers offer vision,
  prefer same-billing-type (subscription) vision models before pay-per-token
  ones — `BillingTypeResolved()` data exists in the registry.

### Phase 4 — Designer-facing surfaces (WebUI parity + prompt posture)

- **WebUI inline parity:** paste/drag/attach paths resolve through the same
  resolver (`/api/query` message assembly shares the agent's inline path);
  delegated/OCR substitutions render with distinct transcript styling.
- **Prompt posture flip:** system prompts (both variants) stop being
  defensive ("never read_file a binary image" guidance stays) and gain the
  assumed-vision framing: images arrive inline by default; bracketed
  provenance means degraded; when a description is needed at higher fidelity,
  re-run `analyze_image_content` rather than asking the user to re-paste.

Batch ordering, image-compare hints, and other designer-flow refinements
belong to the designer spec, not here — this phase ships only resolver
parity, provenance rendering, and the prompt flip.

## Acceptance criteria

- [x] Single resolver answers capability for any (provider, model);
      `effectiveVisionSupport`, `conversation.go`'s
      `supportsConversationalVision`, `pkg/agent/vision_batch_split.go`, and
      the `SupportsConversationalVision` interface method no longer exist
      (`grep` clean), and the test fakes shrink accordingly. *(Phase 1,
      2026-09-18.)*
- [x] Config rule 3 (silent optimism) is gone; unlisted-model behavior is
      optimistic-unknown **with verification**, covered by a scripted-client
      test where the provider 400s images and the turn still completes via
      delegation. *(Phase 2, 2026-09-18: verify-and-remember shipped with
      text-only reretry; the "via delegation" upgrade is Phase 3.)*
- [x] Runtime capability cache persists across restarts, expires per TTL,
      and is a plain hand-editable JSON file in the state dir (no dedicated
      debug command — the file is the surface). *(Phase 2.)*
- [x] Transient errors never write known-false (classifier table tested
      against recorded provider error bodies, synthetic). *(Phase 2.)*
- [ ] Per-model vision limits: `glm-4.5v` and `glm-5v-turbo` resolve
      different `MaxImageCount`/byte budgets without provider-config edits to
      shared fields.
- [ ] `refresh_provider_catalog` emits capabilities from existing tags
      (projection only; no new pipeline).
- [x] Net-LoC gate holds at every phase boundary: the vision tier ends each
      phase smaller than it started (`git diff --stat` on the vision file
      set). *(Phase 1: 68 insertions / 873 deletions across the touched
      vision set.)*
- [x] Non-vision primary + designer-style request: model receives structured
      description with provenance label, not an OCR dump and not an error.
      *(Phase 3: `DelegateImageDescriptions` + provenance labels; scripted
      client test in `vision_delegate_test.go`.)*
- [x] WebUI paste → inline (vision primary) and paste → attributed
      description (non-vision primary) covered; the WebUI rides the same
      agent paste path (structural parity), upload surface covered by
      `test/webui/vision-parity.spec.ts`.
- [ ] Provider-neutrality grep (SP-137's `TestVisionTierNoProviderNames`
      pattern) extended to the new resolver and delegation files.
- [x] Both system prompt variants updated; prompts now teach the
      provenance-label contract (inline default; bracketed = degraded;
      re-analyze for fidelity).

## Open assumptions to validate

Carried as explicit questions, resolved before or during the phase that
depends on them — not silently assumed:

1. **Image lifecycle in conversation history.** Inline images ride the
   message log on every subsequent turn; nothing ages them out
   (`llm_summarizer.go` is text-only — compaction drops image data without
   leaving a trace). Needed: images on disk (`.sprout/pasted-images/`)
   decay to provenance labels after N turns and re-attach on demand, and
   summarization preserves the label text. Decide whether this lives here
   (substrate) or in the designer spec.
2. **Tool-role shrinkage.** After inline becomes the default, the vision
   tools' job narrows to: explicit high-fidelity/structured analysis,
   OCR extraction, PDF page pipeline, and the delegation rung's engine.
   Verify `analyze_image_content`'s general mode still earns its keep as a
   model-invocable tool once ambient `read_file` covers most image
   encounters; delete modes that become dead.
3. **Cross-provider image flow.** Delegation sends user images to a
   different provider than the session chose — a privacy boundary the CLI
   never crossed. Prefer same-provider vision models before external ones
   in the candidate sort; disclose in provenance. Confirm this policy is
   acceptable for the designer feature's user base.
4. **Cost accounting.** Image input is billed at premium input rates on
   pay-per-token providers (zai-coding's subscription is $0-token, which
   hides this). Verify image parts flow into the SP-113 cost footer and
   analytics via `ImageMessageOverheadTokens`, so vision-first defaults
   don't silently inflate bills.
5. **Capability ≠ quality.** The resolver answers *can* see, not *sees
   well*; `glm-4.5v` and `glm-5v-turbo` differ materially for design work.
   Delegation ordering by capability alone may pick a poor describer — the
   designer spec likely needs a quality-ranked model preference, kept out
   of this substrate.
6. **WASM boundary.** Vision tools are stubbed in WASM
   (`vision_stubs_js.go`) and native OCR is unavailable in-browser; inline
   works (HTTP from browser), but the delegation rung needs the
   candidate walk available in `all_vision_js.go`. Confirm daemon-mode
   WebUI covers the designer feature (it does today) and WASM standalone
   degrades to inline-only explicitly, not by error.

## Sequencing & risks

- **Order:** 1 → 2 → 3 → 4. Phases 1–2 are self-contained and testable with
  scripted clients; 3 unlocks the designer UX; 4 is parity/prompt work that
  can trail by a session.
- **Negative-cache poisoning** (transient misclassified as capability):
  the classifier is one conservative function — substring match on 4xx
  bodies only, no learning on 5xx/timeouts — and the negative TTL is short
  (7d). Correction path is editing or deleting the JSON cache file; no
  override config exists to maintain.
- **Cost of optimism:** unknown models get one rejection worth of extra
  latency on first contact, bounded by per-model caps; conversational
  turn-level, not per-image.
- **Seed/foundry contract:** inline-path changes touch sproutProvider message
  assembly — run foundry's `make test-integration` per COMPATIBILITY.md
  before merging phases that alter `core.Message` image handling (expected:
  none, but verify).
- **Deletion passes gate each phase.** A phase ships only with its listed
  deletions landed; "wire now, clean up later" is how the tier got to ~30
  files.
