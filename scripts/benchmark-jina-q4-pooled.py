#!/usr/bin/env python3
"""Benchmark q4-pooled Jina export vs the shipped int8-on-GO-pooled path.

Measures single-embed latency, batch throughput, and output-file size for:
- Baseline: `model_quantized.onnx` (int8 dynamic) + mean-pooling done in Go
- New:      `model_q4.onnx` (MatMulNBits) + mean-pooling baked into the graph

Run AFTER export-jina-q4-pooled.py has populated models/jina-code-v2-q4-pooled/.

Prints a comparison table. The "new" path should be materially faster on
ORT CPU because (a) q4 weight-only beats int8 dynamic on transformer
inference, and (b) baking pooling into the graph eliminates the
[batch, seq, 768] -> [batch, 768] cgo round-trip sprout's Go provider
currently pays per batch.

Usage:
    python3 scripts/benchmark_jina_q4_pooled.py
"""

from __future__ import annotations

import json
import statistics
import sys
import time
from pathlib import Path

EMB_DIR = Path(__file__).resolve().parent.parent
MODEL_ID = "jinaai/jina-embeddings-v2-base-code"
Q4_DIR = EMB_DIR / "models" / "jina-code-v2-q4-pooled"

CORPUS = [
    "func ReadConfig(path string) (string, error)",
    "def load_yaml(path: str) -> dict",
    "const parseJSON = (text) => JSON.parse(text)",
    "function debounce(fn, ms) { /* ... */ }",
    "public class HttpClient { public get(url) {} }",
    "impl Database { fn query(&self, sql: &str) -> Vec<Row> }",
    "export async function fetchUser(id: number): Promise<User>",
    "package main\nfunc main() { fmt.Println(\"hello\") }",
    "async function* iterate(items) { for (const i of items) yield await f(i) }",
    "template<typename T> T max(T a, T b) { return a > b ? a : b; }",
] * 20  # 200 texts total


def benchmark(session, tokenizer, pool_in_python: bool, batch_sizes=(1, 8, 32)):
    """Returns dict: {batch_size: {latency_us, throughput_rec_s}}"""
    import numpy as np
    input_names = [i.name for i in session.get_inputs()]
    needs_tti = "token_type_ids" in input_names
    results = {}
    for bs in batch_sizes:
        batches = [CORPUS[i:i + bs] for i in range(0, len(CORPUS), bs)]
        # Warm
        for b in batches[:2]:
            tok = tokenizer(b, return_tensors="np", padding=True, truncation=True, max_length=512)
            feeds = {k: v for k, v in tok.items() if k in input_names}
            if needs_tti:
                feeds["token_type_ids"] = np.zeros_like(tok["input_ids"])
            session.run(None, feeds)

        latencies = []
        for b in batches:
            tok = tokenizer(b, return_tensors="np", padding=True, truncation=True, max_length=512)
            feeds = {k: v for k, v in tok.items() if k in input_names}
            if needs_tti:
                feeds["token_type_ids"] = np.zeros_like(tok["input_ids"])
            t0 = time.perf_counter()
            out = session.run(None, feeds)[0]
            latencies.append((time.perf_counter() - t0) * 1e6)

            if pool_in_python and out.ndim == 3:
                mask = tok["attention_mask"][..., None].astype(out.dtype)
                _ = (out * mask).sum(axis=1) / mask.sum(axis=1)

        rec_per_batch = statistics.mean(len(b) for b in batches)
        mean_lat = statistics.mean(latencies)
        results[bs] = {
            "latency_us": mean_lat,
            "throughput_rec_s": rec_per_batch / (mean_lat / 1e6),
        }
    return results


def main() -> None:
    import onnxruntime as ort
    from transformers import AutoTokenizer

    try:
        from optimum.onnxruntime import ORTModelForFeatureExtraction
    except ImportError:
        print("optimum not installed; baseline path will use HF Hub cache model directly", file=sys.stderr)

    print(f"Loading tokenizer from {MODEL_ID}...")
    tokenizer = AutoTokenizer.from_pretrained(MODEL_ID, trust_remote_code=True)

    so = ort.SessionOptions()
    so.intra_op_num_threads = 4

    # Baseline: int8 dynamic (the current sprout default)
    print("Loading baseline model_quantized.onnx (int8)...")
    from huggingface_hub import snapshot_download
    baseline_dir = snapshot_download(MODEL_ID, allow_patterns=["onnx/model_quantized.onnx", "tokenizer*"])
    baseline_path = Path(baseline_dir) / "onnx" / "model_quantized.onnx"
    baseline_session = ort.InferenceSession(str(baseline_path), sess_options=so, providers=["CPUExecutionProvider"])
    print(f"  loaded ({baseline_path.stat().st_size // 1024 // 1024} MB)")

    # New: q4-pooled and q8-pooled
    for qbits, qname in [(4, "q4"), (8, "q8")]:
        q_graph = Q4_DIR / f"model_{qname}.onnx"
        if not q_graph.exists():
            print(f"  model_{qname}.onnx not found, skipping {qname} benchmark")
            continue
        print(f"Loading new model_{qname}.onnx ({qname} + pooled)...")
        globals()[f"{qname}_session"] = ort.InferenceSession(str(q_graph), sess_options=so, providers=["CPUExecutionProvider"])
        q_total = q_graph.stat().st_size
        q_data = Q4_DIR / f"model_{qname}.onnx.data"
        if q_data.exists():
            q_total += q_data.stat().st_size
        print(f"  loaded ({q_total // 1024 // 1024} MB total)")

    print("\nBenchmarking...")
    print("Baseline (int8 + Go-side pooling equivalent):")
    baseline_results = benchmark(baseline_session, tokenizer, pool_in_python=True)
    results_map = {"baseline": baseline_results}
    for qname in ["q4", "q8"]:
        sess = globals().get(f"{qname}_session")
        if sess:
            print(f"New ({qname} + graph-side pooling):")
            results_map[qname] = benchmark(sess, tokenizer, pool_in_python=False)

    # Report
    print("\n" + "=" * 76)
    quant_names = [k for k in results_map if k != "baseline"]
    header = f"{'batch':>6}  {'baseline us':>12}"
    for qn in quant_names:
        header += f"  {qn+' us':>12}"
    print(header)
    print("-" * 76)
    for bs in sorted(baseline_results):
        row = f"{bs:>6}  {baseline_results[bs]['latency_us']:>9.1f} us"
        for qn in quant_names:
            row += f"  {results_map[qn][bs]['latency_us']:>9.1f} us"
        print(row)
    print("=" * 76)
    print("\nSpeedup vs baseline:")
    for bs in sorted(baseline_results):
        b = baseline_results[bs]["latency_us"]
        for qn in quant_names:
            q = results_map[qn][bs]["latency_us"]
            print(f"  batch={bs:>3}: baseline→{qn}: {b/q:.2f}x")

    print("\nThroughput (records/sec):")
    for bs in sorted(baseline_results):
        row = f"  batch={bs:>3}: baseline {baseline_results[bs]['throughput_rec_s']:>7.1f}"
        for qn in quant_names:
            row += f"  {qn} {results_map[qn][bs]['throughput_rec_s']:>7.1f}"
        print(row)

    # Save JSON
    out_path = Q4_DIR / "benchmark.json"
    out_path.write_text(json.dumps({
        **{f"baseline_int8": baseline_results},
        **{f"{qn}_pooled": results_map[qn] for qn in quant_names},
        "baseline_size_mb": baseline_path.stat().st_size // 1024 // 1024,
    }, indent=2))
    print(f"\nSaved: {out_path}")


if __name__ == "__main__":
    main()
