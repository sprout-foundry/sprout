# SP-135 — Code-Specific Embedding Model (smaller than Gemma, trained on ~100k repos)

## Problem

sprout runs every embedding task through one model, EmbeddingGemma-300M (q4, ONNX): code retrieval, duplicate detection, conversation memory. Gemma is a generalist trained mostly on natural language — its code behavior is a byproduct. The user wants a **code-only embedding model, smaller than 300M, trained on ~100k code repos**, used for code retrieval + duplicate detection, with Gemma kept for NL semantics (conversation memory, file-level NL indexing). Scope: data, architecture, training, integration, and whether 100k repos is enough.

## Current State

- **Swap is easy; stores are not.** `EmbeddingProvider` is clean (`provider.go:12`), but `ModelHash()` keys every store (`manifest.go:122` invalidates on change) — any swap forces a full re-embed (~30 min for ~12k units). Two-store routing is the design question, not the provider.
- **Prefixes are part of Gemma's geometry** (`index.go:19-26`): documents `"title: none | text: "`, code queries `"task: code retrieval | query: "`, NL queries `"task: search result | query: "`. A new model brings its own subspace; its prefixes (or lack of them) must be re-derived, not copied.
- **Thresholds are model-specific** (`constants.go`): dup 0.65, related 0.55, search 0.40, conversation 0.45. Bands: near-dup 0.767 / related 0.492 / unrelated 0.355 (symmetric doc prefix); correct code-search hits 0.499-0.613, wrong ~0.32 (`codeQueryPrefix`). Seed corpus: `parity_probe_test.go` (near-dup pair + 96 units + mixed-store), `retrieval_quality_test.go` (10 NL→code queries), `prefix_symmetry_test.go` — opt-in (`SPROUT_RETRIEVAL_EVAL=1`).
- **ONNX pipeline exists** (`model_downloader.go:341` pinning, `onnx_embedding_provider.go` DynamicAdvancedSession, maxSeqLen 2048). Gemma's export is hostile to CoreML EP (SP-134: com.microsoft ops, 262k vocab); a BERT-class code model avoids most of those blockers — a bonus, not the goal here.

## Data: what 100k repos actually buys

- **Scale reality.** The Stack v2 = 3.28B unique files from **104.2M** GitHub repos (SWH 2023-09-06 graph), 67.5TB full / 32.1TB dedup, ~900B tokens in train-full (HF card; arxiv.org/abs/2402.19173). So 100k repos ≈ 900B × 100k/104.2M ≈ **0.9B tokens raw** (~0.5B after dedup). The earlier "2-5B" estimate assumed 32M repos; the crawl is 3× larger.
- **License filtering.** The Stack v1's permissive-only claim was contested (weak copyleft MPL/EPL/LGPL included; DMCA/Copilot litigation noise). v2: repo license from GHArchive (only 3.07% of repos), otherwise ScanCode file-level detection; permissive + unlicensed only, excludes copyleft/commercial; opt-outs honored via versioned removals (v2.0.1/v2.1.0/v2.2.0). Replicate if you crawl yourself.
- **Pair yield.** CodeSage v1: 367.9M functions, 105.8M with docstrings, 75.4M with usable summaries (Table 4, arxiv.org/abs/2402.01935). CoRNStack: ~297M raw pairs → **21.2M after dual consistency filtering** (top-k=2, δ=0.7; ~7%) (arxiv.org/abs/2412.01007). A 100k-repo subset of *well-documented, popular* repos → ~5-15M raw pairs → **0.5-1.5M curated pairs** (~2-7% of CoRNStack).
- **Pipeline options.** (a) **HuggingFace — no crawl**: `bigcode/the-stack-v2` (SWHIDs), `the-stack-v2-dedup`, already-mined contrastive sets `nomic-ai/cornstack-{python,java,javascript,go,php,ruby}-v1` (2.8M-23.6M rows incl. negatives, Apache-2.0). (b) GHArchive + clone (curated top-repo sample). (c) Software Heritage graph (what BigCode did) — if you need provenance not on HF.

## Architecture

- **Target: 60-137M BERT-class encoder-only.** CodeRankEmbed 137M, CodeSage-small 130M, Jina-Code-v2 161M. Gemma's 262k vocab costs ~200M params in the embed table alone; a 50k-vocab BERT table is ~38M — smaller model AND smaller q4 file (137M fp16 ≈ 275 MB, q4 ≈ 80 MB vs Gemma's 617/197 MB).
- **Dim 768** (matches current vectors; MRL only if starting from CodeSage-v2, which supports 64-1024). **Context 2048** — sprout truncates at `MaxBodyLen=2000` and maxSeqLen 2048 today; 8192 (Jina/CodeRankEmbed via ALiBi) is nice-to-have, costs O(n²).
- **Pooling**: mean pooling for Jina/CodeRankEmbed, CLS for CodeSage — follow the checkpoint. **Prefixes**: CodeRankEmbed requires "Represent this query for searching relevant code"; CodeSage/Jina are symmetric — drop Gemma's task prefixes then.
- **Start from an existing checkpoint — always.** All three are Apache-2.0/MIT, code-trained on the full Stack v2, small enough to fine-tune on one GPU.

## Training

- **Objective**: InfoNCE on (text, code) pairs with in-batch + mined hard negatives. CoRNStack recipe: dual consistency filtering, softmax negative sampling over the corpus similarity matrix, false-negative filter γ=0.95, curriculum τ′ 0.05→0.001, batch 8192, τ=0.07. Cheaper fallback: BM25 + in-batch only. CodeSage-v2: consistency filtering removed 40% of pairs → +10% Code2Code, +3% NL2Code — **filtering quality beats raw quantity** (codesage-v2.html).
- **Compute** (6·P·T FLOPs): from scratch at CodeSage scale (524B-token MLM + CL) ≈ **1.5K A100-hours** (~$1.5-4.5K cloud). Fine-tune-only on 1-5B pair tokens ≈ **15-40 A100-hours**; 0.5-1.5M curated pairs ≈ a day or two on one A100/RTX-4090. Nomic's `contrastors` (Apache-2.0) implements the mining pipeline.
- **Eval**: CodeSearchNet (2M+ pairs, 6 langs, MRR), CoSQA (20,604 web-query pairs), CoIR (10 datasets, NDCG@10), **plus sprout's regression corpus** (parity-probe bands + retrieval_quality's 10 queries). Sprout corpus is the shipping gate; CSN/CoSQA guard against overfitting.

## Integration

- **Provider swap**: new ONNX provider implementing `EmbeddingProvider` from a BERT export; same sha256-pinned downloader pattern (`model_downloader.go:341`).
- **Dual-model routing**: the code model owns the **code index** (documentPrefix docs, codeQueryPrefix queries, duplicate detection); Gemma keeps the **conversation store** (no prefix) + NL file-level indexing. Two stores keyed by different ModelHashes → swap one without re-embedding the other; conversation thresholds untouched. Cost: both models resident (~80 + 197 MB q4) and duplicated plumbing.
- **Thresholds**: re-measure with the parity-probe methodology — embed the regression corpus, print bands, re-derive dup/search gates. Do NOT reuse 0.65/0.40/0.45; they describe Gemma's geometry, not the new model's.
- **Quantization**: q4 via MatMulNBits as today; verify q4-vs-fp16 parity before shipping (BPE over code is unforgiving to quant noise). BERT exports use standard LayerNormalization/GELU, so SP-134's CoreML blockers largely vanish — a future CoreML-EP candidate, out of scope here.

## Recommendation

- **Verdict: 100k repos is NOT enough to beat EmbeddingGemma-300M on code retrieval with a from-scratch model.** 0.9B tokens ≈ 1/500th of CodeSage's 524B-token pretraining. **But from-scratch is the wrong question**: an *adopted or fine-tuned* 137M code model is cheap and plausibly better because it is purpose-built — CodeRankEmbed: CSN MRR 77.9 at 137M vs CodeSage-large's 71.2 at 1.3B (model card).
- **Go/no-go**: **GO** on "small code model replaces Gemma for code retrieval + duplicate detection" via adopt-then-fine-tune. **NO-GO** on pretraining from 100k repos from scratch unless a specific need (data ownership, license control, exotic languages) justifies ~1.5K A100-hours for expected-worse-than-CoRNStack quality.
- **Phases**:
  1. **Phase 0 (1-2 days)**: adopt CodeRankEmbed or CodeSage-small-v2 as-is; ONNX export + q4; measure on the sprout regression corpus. Gate: beats Gemma's separation (near-dup-vs-unrelated gap; correct-hit floor) on ≥8/10 retrieval cases. If yes, ship dual-model and stop.
  2. **Phase 1 (1-2 weeks)**: fine-tune the Phase-0 model on a 100k-repo curated subset or directly on `nomic-ai/cornstack-*-v1` (InfoNCE + BM25/in-batch negatives); eval CSN/CoSQA + sprout corpus; re-measure thresholds. Gate: ≥5% MRR/NDCG gain over Phase 0 OR ≥0.03 separation gain on sprout bands.
  3. **Phase 2 (only if Phase 1 still loses to Gemma)**: MLM pretrain from scratch on the 100k-repo corpus (20-50B tokens) then CL — 100-400 A100-hours, with a written failure-mode diagnosis from Phase 1.

## Risks

1. **Store invalidation**: any swap invalidates indexes — one ~30 min rebuild per swap, say so in release notes.
2. **Prefix/geometry mismatch**: thresholds are model-specific; skipping re-measurement silently breaks dup/search gates (the original 0.90/0.85 defect pattern).
3. **Corpus skew**: top-starred-repo selection over-represents Python/JS and under-represents Go/Rust, which is what sprout indexes — stratify the sample.
4. **Export friction**: Jina/CodeRankEmbed need `trust_remote_code` and custom pooling (ALiBi, mean-pool); prefer standard-architecture checkpoints first.
5. **q4 degradation on code tokens**: verify q4-vs-fp16 parity on the regression corpus before shipping (Gemma q4 vs API fp16 already drifts to 0.95 cosine).
6. **Dual-model RAM + plumbing**: two providers, two stores, two threshold sets — keep routing in one place behind `EmbeddingProvider`.
7. **Jina v2 base code: keep int8 dynamic, don't switch to MatMulNBits.** Measured 2026-08-06 on ai-workstation (RTX PRO 6000). The shipped `model_quantized.onnx` (int8 dynamic, 154 MB) outperforms q4 and q8 MatMulNBits on both parity and speed: q4 fails parity (0.9450 cosine, needs more than 4 bits/weight), and even q8 (0.9998 cosine) is 6× slower and 2× larger than int8 dynamic. The "q4 wins on CPU" pattern from Gemma does not transfer — Jina's BERT+GEGLU+ALiBi op mix is different. The one real perf win available is **baking mean pooling into the ONNX graph** instead of doing it in Go (`jina_provider.go:runInference` + `runInferenceBatch`): the upstream graph returns `last_hidden_state [batch, seq_len, 768]`, forcing a cgo round-trip on the full 3D tensor per batch. sentence-transformers' export path bakes the configured `emb_pooler: mean` into the graph (fp32-pooled parity = 1.0000); use `scripts/export-jina-q4-pooled.py` as the basis for a future int8-dynamic-pooled re-export.

## Out of scope

- Reranking (CodeRankLLM / Nomic Embed Code 7B) — retrieval-only here; revisit if recall@k disappoints.
- CoreML/GPU acceleration for the new model (SP-134 owns the Gemma path; revisit if Phase 0 picks a BERT checkpoint).
- NL/memory embeddings — Gemma keeps those; this spec only routes code retrieval + duplicate detection to the code model.
- Legal opinion on training-data licensing — records the datasets' claims (Apache-2.0/MIT, Stack v2 opt-out process); get counsel before shipping a model whose training redistributes code.
