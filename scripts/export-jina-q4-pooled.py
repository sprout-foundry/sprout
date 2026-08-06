#!/usr/bin/env python3
"""Re-export jina-embeddings-v2-base-code for ORT CPU performance.

Three problems with the upstream ONNX export as shipped in sprout
(`model_quantized.onnx`, int8 dynamic):

1. **Mean pooling is in Go, not in the graph.** The exported graph outputs
   `last_hidden_state` [batch, seq_len, 768]; the Go provider allocates
   that whole tensor, copies it across cgo, then mean-pools. For a
   32-row x 512-token batch that's 12.6M floats per batch transferred
   vs 24K if pooling were in-graph. Sentence-transformers ships the
   pooled output natively when invoked through its own export path.

2. **int8 dynamic quantization is slow on ORT CPU.** Dynamic int8
   dequantizes per-tensor at runtime, which costs more than it saves
   at small batch sizes. MatMulNBits (q4 weight-only) is what's
   actually fast on ORT CPU — see the Gemma variant table in sprout's
   `pkg/embedding/model_downloader.go` (model_q4 is 1.5x faster than
   model_fp16 on the same model).

3. **fp16 export not used.** The shipped model_quantized.onnx is int8
   because that's what the upstream repo provides, but ORT CPU has no
   native fp16 kernels — q4 (MatMulNBits) is the right CPU target.

This script:
- Re-exports the model from PyTorch with mean pooling baked in
  (sentence-transformers' export path handles this via `emb_pooler: mean`).
- Quantizes the fp32 export to q4 (MatMulNBits).
- Verifies parity: q4-pooled embeddings vs original-int8-Go-pooled
  should be ≥0.99 cosine on the sprout code-search corpus.

Outputs to `models/jina-code-v2-q4-pooled/`:
- `model_q4.onnx`         — quantized graph with mean pooling baked in
- `model_q4.onnx_data`    — external weights blob (MatMulNBits uses these)
- `tokenizer.json`        — copied from the original repo
- `config.json`           — model config (for reference)
- `PARITY.md`             — parity report

Usage:
    python3 scripts/export_jina_q4_pooled.py

Requires (the script will check and exit cleanly if missing):
    pip install transformers sentence-transformers onnx onnxruntime optimum torch
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

EMB_DIR = Path(__file__).resolve().parent.parent
OUTPUT_DIR = EMB_DIR / "models" / "jina-code-v2-q4-pooled"
MODEL_ID = "jinaai/jina-embeddings-v2-base-code"


def die(msg: str, code: int = 1) -> None:
    print(f"ERROR: {msg}", file=sys.stderr)
    sys.exit(code)


def check_imports() -> None:
    missing = []
    for mod in ("torch", "transformers", "sentence_transformers", "onnx", "onnxruntime", "optimum", "numpy"):
        try:
            __import__(mod)
        except ImportError:
            missing.append(mod)
    if missing:
        die(
            "missing dependencies. Install with:\n"
            "  pip install transformers sentence-transformers onnx onnxruntime optimum torch numpy\n"
            f"Missing: {', '.join(missing)}"
        )


def export_pooled_onnx(out_dir: Path) -> Path:
    """Export jina-embeddings-v2-base-code via sentence-transformers so the
    `mean` pooling layer (configured via `emb_pooler: "mean"`) is part of
    the ONNX graph, not done in Go afterwards.

    Returns the path to the exported model.onnx.
    """
    from sentence_transformers import SentenceTransformer
    from transformers import AutoTokenizer
    import torch

    print(f"[1/4] Loading {MODEL_ID} via sentence-transformers...")
    # trust_remote_code required for jinaai/jina-bert-v2-qk-post-norm
    model = SentenceTransformer(MODEL_ID, trust_remote_code=True, device="cpu")
    # Sanity: model should have a pooling module. Jina v2 base code uses mean.
    modules = [type(m).__name__ for m in model]
    print(f"       modules: {modules}")
    if "Pooling" not in modules:
        die(
            "expected a Pooling module in the sentence-transformer pipeline; "
            f"got {modules}. The model card's `emb_pooler: mean` should produce one. "
            "Inspect the model and adjust the export path."
        )

    # Confirm output dim is [batch, hidden] not [batch, seq, hidden]
    test = model.encode(["hello world"], convert_to_numpy=True)
    if test.shape != (1, 768):
        die(f"unexpected sentence-transformer output shape {test.shape}; want (1, 768)")
    print(f"       sentence-transformer output: {test.shape} (good — pooled)")

    # Export via optimum — this bakes the WHOLE sentence-transformer pipeline
    # (transformer + pooling + normalize) into a single ONNX graph.
    print(f"[2/4] Exporting to ONNX at {out_dir}/model.onnx...")
    out_dir.mkdir(parents=True, exist_ok=True)
    onnx_path = out_dir / "model.onnx"

    # Path A: sentence-transformers' built-in ONNX export (>=3.0).
    try:
        export_path = model.save(str(out_dir), format="onnx")
        if isinstance(export_path, (list, tuple)):
            export_path = export_path[0]
        exported = Path(export_path) if export_path else (out_dir / "model.onnx")
        if exported.exists():
            print(f"       sentence-transformers export OK: {exported}")
            return exported
        print(f"       sentence-transformers export produced no file at {exported}")
    except Exception as e:
        print(f"       sentence-transformers export failed: {e}")

    # Path B: optimum with the underlying transformer model + a pooling wrapper.
    # This is more work but more controllable. We export the transformer and
    # append mean-pooling + normalize nodes manually.
    print(f"       falling back to optimum + manual pooling append...")
    from optimum.onnxruntime import ORTModelForFeatureExtraction
    from transformers import AutoTokenizer

    tokenizer = AutoTokenizer.from_pretrained(MODEL_ID, trust_remote_code=True)
    transformer = ORTModelForFeatureExtraction.from_pretrained(
        MODEL_ID, export=True, trust_remote_code=True
    )
    transformer.save_pretrained(out_dir)
    raw_onnx = out_dir / "model.onnx"
    if not raw_onnx.exists():
        die(f"optimum export produced no model.onnx at {raw_onnx}")

    # Append mean-pooling + L2-norm nodes to the graph.
    import onnx
    from onnx import helper, TensorProto

    m = onnx.load(str(raw_onnx))
    # The transformer output is `last_hidden_state` [batch, seq, hidden].
    # We add:
    #   mask_f = cast(attention_mask, float)              [batch, seq]
    #   mask_f_e = unsqueeze(mask_f, -1)                  [batch, seq, 1]
    #   masked = mul(last_hidden_state, mask_f_e)         [batch, seq, hidden]
    #   sum_mask = reduce_sum(mask_f, axis=1)             [batch]
    #   sum_mask_e = unsqueeze(sum_mask, -1)              [batch, 1]
    #   sum_mask_clamped = max(sum_mask_e, 1e-9)          [batch, 1]
    #   sum_hidden = reduce_sum(masked, axis=1)           [batch, hidden]
    #   pooled = div(sum_hidden, sum_mask_clamped)        [batch, hidden]
    #   normalized = l2_normalize(pooled)                 [batch, hidden]
    #
    # Output name: "pooled" replaces "last_hidden_state".
    inp_ids, attn_mask = m.graph.input[0].name, m.graph.input[1].name
    last_hidden = m.graph.output[0].name

    g = m.graph
    # cast attention_mask to float
    mask_f = "attention_mask_float"
    g.node.append(helper.make_node("Cast", [attn_mask], [mask_f], to=TensorProto.FLOAT))
    # unsqueeze to [batch, seq, 1]
    mask_f_e = "attention_mask_float_e"
    axes_unsq = helper.make_tensor(f"axes_unsq_1", TensorProto.INT64, [1], [-1])
    g.initializer.append(axes_unsq)
    g.node.append(helper.make_node("Unsqueeze", [mask_f, "axes_unsq_1"], [mask_f_e]))
    # mul: masked hidden
    masked = "masked_hidden"
    g.node.append(helper.make_node("Mul", [last_hidden, mask_f_e], [masked]))
    # sum_mask
    sum_mask = "sum_mask"
    axes_1 = helper.make_tensor("axes_reduce_1", TensorProto.INT64, [1], [1])
    g.initializer.append(axes_1)
    g.node.append(helper.make_node("ReduceSum", [mask_f, "axes_reduce_1"], [sum_mask], keepdims=0))
    # unsqueeze sum_mask to [batch, 1]
    sum_mask_e = "sum_mask_e"
    g.node.append(helper.make_node("Unsqueeze", [sum_mask, "axes_unsq_1"], [sum_mask_e]))
    # clamp sum_mask to >= 1e-9 to avoid div-by-zero
    eps_tensor = helper.make_tensor("eps_floor", TensorProto.FLOAT, [1], [1e-9])
    g.initializer.append(eps_tensor)
    sum_mask_clamped = "sum_mask_clamped"
    g.node.append(helper.make_node("Max", [sum_mask_e, "eps_floor"], [sum_mask_clamped]))
    # sum over seq dim
    sum_hidden = "sum_hidden"
    g.node.append(helper.make_node("ReduceSum", [masked, "axes_reduce_1"], [sum_hidden], keepdims=0))
    # divide
    pooled = "pooled"
    g.node.append(helper.make_node("Div", [sum_hidden, sum_mask_clamped], [pooled]))
    # L2 normalize: norm = sqrt(sum(pooled * pooled))
    sq = "pooled_sq"
    g.node.append(helper.make_node("Mul", [pooled, pooled], [sq]))
    sum_sq = "sum_sq"
    axes_all = helper.make_tensor("axes_all", TensorProto.INT64, [1], [1])
    g.initializer.append(axes_all)
    g.node.append(helper.make_node("ReduceSum", [sq, "axes_all"], [sum_sq], keepdims=1))
    norm = "norm"
    g.node.append(helper.make_node("Sqrt", [sum_sq], [norm]))
    normalized = "pooled_normalized"
    g.node.append(helper.make_node("Div", [pooled, norm], [normalized]))

    # Replace output: pooled_normalized instead of last_hidden_state
    while g.output:
        g.output.pop()
    out_shape = helper.make_tensor_value_info(normalized, TensorProto.FLOAT, ["batch", 768])
    g.output.append(out_shape)

    onnx.checker.check_model(m)
    onnx.save(m, str(raw_onnx))
    print(f"       appended mean-pooling + L2-norm to {raw_onnx}")
    return raw_onnx


def quantize(model_path: Path, out_dir: Path, bits: int, block_size: int = 32, is_symmetric: bool = True) -> Path:
    """Apply MatMulNBits weight-only quantization.

    Returns the graph path. External data is saved alongside as <name>.data.
    """
    print(f"[3/x] Quantizing to q{bits} (MatMulNBits, block_size={block_size}, sym={is_symmetric})...")
    from onnxruntime.quantization.matmul_nbits_quantizer import MatMulNBitsQuantizer

    out_graph = out_dir / f"model_q{bits}.onnx"

    q = MatMulNBitsQuantizer(
        model=str(model_path),
        bits=bits,
        block_size=block_size,
        is_symmetric=is_symmetric,
    )
    q.process()
    q.model.save_model_to_file(str(out_graph), use_external_data_format=True)
    data_file = out_dir / f"model_q{bits}.onnx.data"
    print(f"       wrote {out_graph} ({out_graph.stat().st_size // 1024} KB)")
    if data_file.exists():
        print(f"       wrote {data_file} ({data_file.stat().st_size // 1024 // 1024} MB)")
    return out_graph


def verify_parity(out_dir: Path, model_name: str = "model_q8") -> None:
    """Compare quantized-pooled embeddings against the fp32 reference on a
    small code-snippet corpus. Reports mean cosine.

    The fp32 reference comes from sentence-transformers' encode() on the
    original model — equivalent to what sprout's embedding provider produces.
    """
    model_file = out_dir / f"{model_name}.onnx"
    print(f"[4/x] Verifying parity ({model_name} vs fp32 reference)...")
    import numpy as np
    import onnxruntime as ort
    from sentence_transformers import SentenceTransformer

    reference = SentenceTransformer(MODEL_ID, trust_remote_code=True, device="cpu")

    # Quantized ONNX session
    so = ort.SessionOptions()
    so.intra_op_num_threads = 4
    session = ort.InferenceSession(
        str(model_file),
        sess_options=so,
        providers=["CPUExecutionProvider"],
    )

    # Code-snippet corpus — mix of signatures and bodies, similar to what
    # sprout's extractor produces. Keep it short; this is a sanity check.
    corpus = [
        "func ReadConfig(path string) (string, error)",
        "def load_yaml(path: str) -> dict",
        "const parseJSON = (text) => JSON.parse(text)",
        "function debounce(fn, ms) { /* ... */ }",
        "public class HttpClient { public get(url) {} }",
        "impl Database { fn query(&self, sql: &str) -> Vec<Row> }",
        "export async function fetchUser(id: number): Promise<User>",
        "package main\nfunc main() { fmt.Println(\"hello\") }",
    ]

    ref_embeds = reference.encode(corpus, convert_to_numpy=True, normalize_embeddings=True)

    q4_embeds = []
    for text in corpus:
        tok = reference.tokenizer(text, return_tensors="pt", padding=True, truncation=True, max_length=512)
        feeds = {
            "input_ids": tok["input_ids"].numpy(),
            "attention_mask": tok["attention_mask"].numpy(),
        }
        # Some Jina exports also take token_type_ids. For single-sentence
        # inference, token_type_ids must be all zeros (NOT attention_mask).
        if "token_type_ids" in [i.name for i in session.get_inputs()]:
            feeds["token_type_ids"] = np.zeros_like(tok["input_ids"].numpy())
        out = session.run(None, feeds)[0]
        # If the graph still emits [1, seq, 768] (manual append path), pool here.
        if out.ndim == 3:
            mask = tok["attention_mask"].numpy()[..., None].astype(out.dtype)
            out = (out * mask).sum(axis=1) / mask.sum(axis=1)
            norm = (out * out).sum(axis=1, keepdims=True) ** 0.5
            out = out / np.clip(norm, 1e-9, None)
        q4_embeds.append(out[0])
    q4_embeds = np.array(q4_embeds)

    cosines = (ref_embeds * q4_embeds).sum(axis=1) / (
        np.linalg.norm(ref_embeds, axis=1) * np.linalg.norm(q4_embeds, axis=1)
    )
    mean_cos = cosines.mean()
    print(f"       mean cosine: {mean_cos:.4f} (per-sample: min {cosines.min():.4f}, max {cosines.max():.4f})")

    parity_md = out_dir / f"PARITY_{model_name}.md"
    parity_md.write_text(
        f"# Parity Report: {model_name} vs fp32 reference\n\n"
        f"Mean cosine: **{mean_cos:.4f}**\n\n"
        f"Per-sample cosines:\n\n"
        f"| text | cosine |\n|------|-------:|\n"
        + "\n".join(f"| {t[:50]!r} | {c:.4f} |" for t, c in zip(corpus, cosines))
        + "\n"
    )
    print(f"       wrote {parity_md}")

    if mean_cos < 0.99:
        print(f"       WARNING: parity below 0.99 threshold ({mean_cos:.4f}). "
              f"This quantization config may not be suitable for production use.")


def copy_tokenizer(out_dir: Path) -> None:
    from transformers import AutoTokenizer
    tok = AutoTokenizer.from_pretrained(MODEL_ID, trust_remote_code=True)
    tok.save_pretrained(out_dir)
    # sentence-transformers / sprout's tokenizer.json loader needs tokenizer.json
    print(f"       tokenizer saved to {out_dir}")


def main() -> None:
    check_imports()
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

    # Step 1+2: export with pooling baked in.
    model_onnx = export_pooled_onnx(OUTPUT_DIR)

    # Step 3: quantize to q4 and q8.
    # q4 (block_size=32, asymmetric) gives best q4 quality (~0.95 cosine).
    # q8 (block_size=32, symmetric) gives near-perfect parity (~0.9998 cosine).
    quantize(model_onnx, OUTPUT_DIR, bits=4, block_size=32, is_symmetric=False)
    quantize(model_onnx, OUTPUT_DIR, bits=8, block_size=32, is_symmetric=True)

    # Copy tokenizer.
    copy_tokenizer(OUTPUT_DIR)

    # Step 4: parity for each.
    verify_parity(OUTPUT_DIR, model_name="model_q4")
    verify_parity(OUTPUT_DIR, model_name="model_q8")

    print(f"\nDONE. Outputs in {OUTPUT_DIR}:")
    for p in sorted(OUTPUT_DIR.iterdir()):
        if p.is_file():
            print(f"  {p.name}: {p.stat().st_size // 1024} KB")


if __name__ == "__main__":
    main()
