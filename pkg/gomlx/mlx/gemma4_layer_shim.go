//go:build darwin && arm64 && cgo && mlx

package mlx

/*
#cgo CFLAGS: -DMLX_C_BINDINGS
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lmlx -lmlxc

#include <mlx/c/array.h>
#include <mlx/c/fast.h>
#include <mlx/c/ops.h>
#include <mlx/c/stream.h>
#include <mlx/c/vector.h>

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// Empty mlx_array for NULL-equivalent (zero init).
static const mlx_array MLX_NULL = {0};

// Forward-declare gelu/geglu from gemma4_shim.go (non-static for linkage).
int gemma4_shim_geglu(mlx_array* out, const mlx_array gate, const mlx_array up, const mlx_stream s);
int gemma4_shim_gelu(mlx_array* out, const mlx_array x, const mlx_stream s);

// Quantized matmul helper. Caller owns the returned array (free on done).
static mlx_array do_qmatmul(const mlx_array x, const mlx_array w,
                            const mlx_array scales, const mlx_array biases,
                            int group_size, int bits, const mlx_stream s) {
    mlx_optional_int gs = { .value = group_size, .has_value = 1 };
    mlx_optional_int bs = { .value = bits, .has_value = 1 };
    mlx_array out = {0};
    if (mlx_quantized_matmul(&out, x, w, scales, biases, 1, gs, bs, "affine", s)) {
        if (out.ctx) mlx_array_free(out);
        out.ctx = NULL;
    }
    return out;
}

// RMS norm helper (with optional weight — MLX_NULL means no scale).
static mlx_array do_rmsnorm(const mlx_array x, const mlx_array weight, float eps,
                            const mlx_stream s) {
    mlx_array out = {0};
    if (mlx_fast_rms_norm(&out, x, weight, eps, s)) {
        if (out.ctx) mlx_array_free(out);
        out.ctx = NULL;
    }
    return out;
}

// Concat [a, b] along axis. Caller owns result.
static mlx_array do_concat(const mlx_array a, const mlx_array b, int axis,
                           const mlx_stream s) {
    mlx_array arrs[2] = {a, b};
    mlx_vector_array v = mlx_vector_array_new_data(arrs, 2);
    mlx_array out = {0};
    if (mlx_concatenate_axis(&out, v, axis, s)) {
        mlx_vector_array_free(v);
        if (out.ctx) mlx_array_free(out);
        out.ctx = NULL;
    } else {
        mlx_vector_array_free(v);
    }
    return out;
}

// Transpose [B, S, H, D] -> [B, H, S, D].
static mlx_array do_transpose_bshd(const mlx_array a, const mlx_stream s) {
    int perm[4] = {0, 2, 1, 3};
    mlx_array out = {0};
    if (mlx_transpose_axes(&out, a, perm, 4, s)) {
        if (out.ctx) mlx_array_free(out);
        out.ctx = NULL;
    }
    return out;
}

// RoPE: apply rope to [B, H, S, D]. Full attention uses precomputed freqs.
// Sliding uses standard theta=10000.
static mlx_array do_rope(const mlx_array x, int dims, int is_full,
                         float base_theta, int start_pos,
                         const mlx_array freqs, const mlx_stream s) {
    mlx_array out = {0};
    if (is_full) {
        mlx_optional_float base_opt = { .value = 0, .has_value = 0 };
        if (mlx_fast_rope(&out, x, dims, 0, base_opt, 1.0f, start_pos, freqs, s)) {
            if (out.ctx) mlx_array_free(out);
            out.ctx = NULL;
        }
    } else {
        mlx_optional_float base_opt = { .value = base_theta, .has_value = 1 };
        if (mlx_fast_rope(&out, x, dims, 0, base_opt, 1.0f, start_pos, MLX_NULL, s)) {
            if (out.ctx) mlx_array_free(out);
            out.ctx = NULL;
        }
    }
    return out;
}

// ---------------------------------------------------------------------------
// Full decoder layer with own K/V projections
// ---------------------------------------------------------------------------
//
// Weight pointers are all mlx_array by value (opaque struct, copyable).
// Returns 0 on success, non-zero on MLX error.
// out_h, out_k, out_v are OUTPUT pointers — the caller wraps and frees them.
// ---------------------------------------------------------------------------
static int gemma4_shim_kv_layer(
    mlx_array* out_h, mlx_array* out_k, mlx_array* out_v,
    // Inputs
    const mlx_array h, const mlx_array per_layer_in,
    const mlx_array k_cache, const mlx_array v_cache,
    const mlx_array prop_rope_freqs,
    // Config
    int num_heads, int num_kv_heads, int head_dim,
    int seq_len, int start_pos,
    int is_full, int has_per_layer, int has_layer_scalar,
    float rms_eps,
    int group_size, int bits,
    // Norm weights
    const mlx_array w_input_norm, const mlx_array w_post_attn_norm,
    const mlx_array w_pre_ff_norm, const mlx_array w_post_ff_norm,
    // Q projection + qnorm
    const mlx_array w_qW, const mlx_array w_qS, const mlx_array w_qB,
    const mlx_array w_qnorm,
    // K projection + knorm
    const mlx_array w_kW, const mlx_array w_kS, const mlx_array w_kB,
    const mlx_array w_knorm,
    // V projection
    const mlx_array w_vW, const mlx_array w_vS, const mlx_array w_vB,
    // O projection
    const mlx_array w_oW, const mlx_array w_oS, const mlx_array w_oB,
    // MLP projections
    const mlx_array w_gateW, const mlx_array w_gateS, const mlx_array w_gateB,
    const mlx_array w_upW, const mlx_array w_upS, const mlx_array w_upB,
    const mlx_array w_downW, const mlx_array w_downS, const mlx_array w_downB,
    // Per-layer gating (may be MLX_NULL)
    const mlx_array w_pligW, const mlx_array w_pligS, const mlx_array w_pligB,
    const mlx_array w_plpW, const mlx_array w_plpS, const mlx_array w_plpB,
    const mlx_array w_post_plin_norm,
    // Layer scalar (may be MLX_NULL)
    const mlx_array w_layer_scalar,
    const mlx_stream s)
{
    // --- Input norm ---
    mlx_array normed = do_rmsnorm(h, w_input_norm, rms_eps, s);
    if (!normed.ctx) return -1;

    // --- Q projection + norm + reshape ---
    mlx_array q = do_qmatmul(normed, w_qW, w_qS, w_qB, group_size, bits, s);
    if (!q.ctx) return -1;
    int qshape[4] = {1, seq_len, num_heads, head_dim};
    mlx_array qR = {0};
    if (mlx_reshape(&qR, q, qshape, 4, s)) return -1;
    mlx_array qNormed = do_rmsnorm(qR, w_qnorm, rms_eps, s);
    if (!qNormed.ctx) return -1;

    // --- K projection + norm + transpose ---
    mlx_array k2d = do_qmatmul(normed, w_kW, w_kS, w_kB, group_size, bits, s);
    if (!k2d.ctx) return -1;
    int kvshape[4] = {1, seq_len, num_kv_heads, head_dim};
    mlx_array kR = {0};
    if (mlx_reshape(&kR, k2d, kvshape, 4, s)) return -1;
    mlx_array kNormed = do_rmsnorm(kR, w_knorm, rms_eps, s);
    if (!kNormed.ctx) return -1;
    mlx_array kT = do_transpose_bshd(kNormed, s);
    if (!kT.ctx) return -1;

    // --- V projection + norm (no scale) + transpose ---
    mlx_array v2d = do_qmatmul(normed, w_vW, w_vS, w_vB, group_size, bits, s);
    if (!v2d.ctx) return -1;
    mlx_array vR = {0};
    if (mlx_reshape(&vR, v2d, kvshape, 4, s)) return -1;
    mlx_array vNormed = do_rmsnorm(vR, MLX_NULL, rms_eps, s);
    if (!vNormed.ctx) return -1;
    mlx_array vT = do_transpose_bshd(vNormed, s);
    if (!vT.ctx) return -1;

    // --- RoPE on K ---
    mlx_array kRot = do_rope(kT, head_dim, is_full, 10000.0f, start_pos, prop_rope_freqs, s);
    if (!kRot.ctx) return -1;

    // --- RoPE on Q ---
    mlx_array qT = do_transpose_bshd(qNormed, s);
    if (!qT.ctx) return -1;
    mlx_array qRot = do_rope(qT, head_dim, is_full, 10000.0f, start_pos, prop_rope_freqs, s);
    if (!qRot.ctx) return -1;

    // --- KV cache append ---
    mlx_array kForAttn, vForAttn;
    if (k_cache.ctx) {
        kForAttn = do_concat(k_cache, kRot, 2, s);
        if (!kForAttn.ctx) return -1;
        vForAttn = do_concat(v_cache, vT, 2, s);
        if (!vForAttn.ctx) return -1;
    } else {
        kForAttn = kRot;
        vForAttn = vT;
    }

    // --- SDPA ---
    const char* mask_mode = (seq_len > 1) ? "causal" : "";
    mlx_array ctx = {0};
    if (mlx_fast_scaled_dot_product_attention(&ctx, qRot, kForAttn, vForAttn, 1.0f, mask_mode, MLX_NULL, MLX_NULL, s))
        return -1;

    // --- Output projection ---
    int ctxperm[4] = {0, 2, 1, 3};
    mlx_array ctxT = {0};
    if (mlx_transpose_axes(&ctxT, ctx, ctxperm, 4, s)) return -1;
    int oshape[3] = {1, seq_len, num_heads * head_dim};
    mlx_array ctxF = {0};
    if (mlx_reshape(&ctxF, ctxT, oshape, 3, s)) return -1;
    mlx_array attnOut = do_qmatmul(ctxF, w_oW, w_oS, w_oB, group_size, bits, s);
    if (!attnOut.ctx) return -1;

    // --- Post-attention norm + residual ---
    mlx_array attnNormed = do_rmsnorm(attnOut, w_post_attn_norm, rms_eps, s);
    if (!attnNormed.ctx) return -1;
    mlx_array residual = {0};
    if (mlx_add(&residual, h, attnNormed, s)) return -1;

    // --- MLP: norm + gate + up + GeGLU + down + norm ---
    mlx_array ffNormed = do_rmsnorm(residual, w_pre_ff_norm, rms_eps, s);
    if (!ffNormed.ctx) return -1;
    mlx_array gate = do_qmatmul(ffNormed, w_gateW, w_gateS, w_gateB, group_size, bits, s);
    if (!gate.ctx) return -1;
    mlx_array up = do_qmatmul(ffNormed, w_upW, w_upS, w_upB, group_size, bits, s);
    if (!up.ctx) return -1;
    mlx_array gegluOut = {0};
    if (gemma4_shim_geglu(&gegluOut, gate, up, s)) return -1;
    mlx_array ffOut = do_qmatmul(gegluOut, w_downW, w_downS, w_downB, group_size, bits, s);
    if (!ffOut.ctx) return -1;
    mlx_array ffNormed2 = do_rmsnorm(ffOut, w_post_ff_norm, rms_eps, s);
    if (!ffNormed2.ctx) return -1;
    mlx_array h2 = {0};
    if (mlx_add(&h2, residual, ffNormed2, s)) return -1;

    // --- Per-layer gating (optional) ---
    if (has_per_layer && per_layer_in.ctx) {
        mlx_array plGate = do_qmatmul(h2, w_pligW, w_pligS, w_pligB, group_size, bits, s);
        if (!plGate.ctx) return -1;
        mlx_array plGelu = {0};
        if (gemma4_shim_gelu(&plGelu, plGate, s)) return -1;
        mlx_array plMul = {0};
        if (mlx_multiply(&plMul, plGelu, per_layer_in, s)) return -1;
        mlx_array plProj = do_qmatmul(plMul, w_plpW, w_plpS, w_plpB, group_size, bits, s);
        if (!plProj.ctx) return -1;
        mlx_array plNormed = do_rmsnorm(plProj, w_post_plin_norm, rms_eps, s);
        if (!plNormed.ctx) return -1;
        mlx_array plRes = {0};
        if (mlx_add(&plRes, h2, plNormed, s)) return -1;
        h2 = plRes;
    }

    // --- Layer scalar (optional) ---
    if (has_layer_scalar && w_layer_scalar.ctx) {
        mlx_array scaled = {0};
        if (mlx_multiply(&scaled, h2, w_layer_scalar, s)) return -1;
        h2 = scaled;
    }

    // --- Set outputs (caller wraps and owns them) ---
    *out_h = h2;
    if (k_cache.ctx) {
        *out_k = kForAttn;
        *out_v = vForAttn;
    } else {
        out_k->ctx = NULL;
        out_v->ctx = NULL;
    }
    // Intermediates are NOT freed here — MLX refcounting + Go finalizers
    // handle cleanup. Manual freeing caused segfaults with lazy eval; forced
    // eval per layer killed performance (24 tok/s vs 45 without).
    return 0;
}

// ---------------------------------------------------------------------------
// KV-shared decoder layer (no K/V projections — reuses from previous layer)
// ---------------------------------------------------------------------------
static int gemma4_shim_shared_kv_layer(
    mlx_array* out_h,
    // Inputs
    const mlx_array h,
    const mlx_array k_for_attn, const mlx_array v_for_attn,
    const mlx_array prop_rope_freqs,
    const mlx_array per_layer_in,
    // Config
    int num_heads, int num_kv_heads, int head_dim,
    int seq_len, int start_pos,
    int is_full, int has_per_layer, int has_layer_scalar,
    float rms_eps,
    int group_size, int bits,
    // Norm weights
    const mlx_array w_input_norm, const mlx_array w_post_attn_norm,
    const mlx_array w_pre_ff_norm, const mlx_array w_post_ff_norm,
    // Q projection + qnorm
    const mlx_array w_qW, const mlx_array w_qS, const mlx_array w_qB,
    const mlx_array w_qnorm,
    // O projection
    const mlx_array w_oW, const mlx_array w_oS, const mlx_array w_oB,
    // MLP projections
    const mlx_array w_gateW, const mlx_array w_gateS, const mlx_array w_gateB,
    const mlx_array w_upW, const mlx_array w_upS, const mlx_array w_upB,
    const mlx_array w_downW, const mlx_array w_downS, const mlx_array w_downB,
    // Per-layer gating (may be MLX_NULL)
    const mlx_array w_pligW, const mlx_array w_pligS, const mlx_array w_pligB,
    const mlx_array w_plpW, const mlx_array w_plpS, const mlx_array w_plpB,
    const mlx_array w_post_plin_norm,
    // Layer scalar (may be MLX_NULL)
    const mlx_array w_layer_scalar,
    const mlx_stream s)
{
    // --- Input norm ---
    mlx_array normed = do_rmsnorm(h, w_input_norm, rms_eps, s);
    if (!normed.ctx) return -1;

    // --- Q projection + norm ---
    mlx_array q = do_qmatmul(normed, w_qW, w_qS, w_qB, group_size, bits, s);
    if (!q.ctx) return -1;
    int qshape[4] = {1, seq_len, num_heads, head_dim};
    mlx_array qR = {0};
    if (mlx_reshape(&qR, q, qshape, 4, s)) return -1;
    mlx_array qNormed = do_rmsnorm(qR, w_qnorm, rms_eps, s);
    if (!qNormed.ctx) return -1;

    // --- Q transpose + RoPE ---
    mlx_array qT = do_transpose_bshd(qNormed, s);
    if (!qT.ctx) return -1;
    mlx_array qRot = do_rope(qT, head_dim, is_full, 10000.0f, start_pos, prop_rope_freqs, s);
    if (!qRot.ctx) return -1;

    // --- SDPA with shared K/V ---
    const char* mask_mode = (seq_len > 1) ? "causal" : "";
    mlx_array ctx = {0};
    if (mlx_fast_scaled_dot_product_attention(&ctx, qRot, k_for_attn, v_for_attn, 1.0f, mask_mode, MLX_NULL, MLX_NULL, s))
        return -1;

    // --- Output projection ---
    int ctxperm[4] = {0, 2, 1, 3};
    mlx_array ctxT = {0};
    if (mlx_transpose_axes(&ctxT, ctx, ctxperm, 4, s)) return -1;
    int oshape[3] = {1, seq_len, num_heads * head_dim};
    mlx_array ctxF = {0};
    if (mlx_reshape(&ctxF, ctxT, oshape, 3, s)) return -1;
    mlx_array attnOut = do_qmatmul(ctxF, w_oW, w_oS, w_oB, group_size, bits, s);
    if (!attnOut.ctx) return -1;

    // --- Post-attention norm + residual ---
    mlx_array attnNormed = do_rmsnorm(attnOut, w_post_attn_norm, rms_eps, s);
    if (!attnNormed.ctx) return -1;
    mlx_array residual = {0};
    if (mlx_add(&residual, h, attnNormed, s)) return -1;

    // --- MLP ---
    mlx_array ffNormed = do_rmsnorm(residual, w_pre_ff_norm, rms_eps, s);
    if (!ffNormed.ctx) return -1;
    mlx_array gate = do_qmatmul(ffNormed, w_gateW, w_gateS, w_gateB, group_size, bits, s);
    if (!gate.ctx) return -1;
    mlx_array up = do_qmatmul(ffNormed, w_upW, w_upS, w_upB, group_size, bits, s);
    if (!up.ctx) return -1;
    mlx_array gegluOut = {0};
    if (gemma4_shim_geglu(&gegluOut, gate, up, s)) return -1;
    mlx_array ffOut = do_qmatmul(gegluOut, w_downW, w_downS, w_downB, group_size, bits, s);
    if (!ffOut.ctx) return -1;
    mlx_array ffNormed2 = do_rmsnorm(ffOut, w_post_ff_norm, rms_eps, s);
    if (!ffNormed2.ctx) return -1;
    mlx_array h2 = {0};
    if (mlx_add(&h2, residual, ffNormed2, s)) return -1;

    // --- Per-layer gating ---
    if (has_per_layer && per_layer_in.ctx) {
        mlx_array plGate = do_qmatmul(h2, w_pligW, w_pligS, w_pligB, group_size, bits, s);
        if (!plGate.ctx) return -1;
        mlx_array plGelu = {0};
        if (gemma4_shim_gelu(&plGelu, plGate, s)) return -1;
        mlx_array plMul = {0};
        if (mlx_multiply(&plMul, plGelu, per_layer_in, s)) return -1;
        mlx_array plProj = do_qmatmul(plMul, w_plpW, w_plpS, w_plpB, group_size, bits, s);
        if (!plProj.ctx) return -1;
        mlx_array plNormed = do_rmsnorm(plProj, w_post_plin_norm, rms_eps, s);
        if (!plNormed.ctx) return -1;
        mlx_array plRes = {0};
        if (mlx_add(&plRes, h2, plNormed, s)) return -1;
        h2 = plRes;
    }

    // --- Layer scalar ---
    if (has_layer_scalar && w_layer_scalar.ctx) {
        mlx_array scaled = {0};
        if (mlx_multiply(&scaled, h2, w_layer_scalar, s)) return -1;
        h2 = scaled;
    }

    *out_h = h2;
    return 0;
}
*/
import "C"

import "fmt"

// LinearHandles holds the quantized weight handles for a Linear layer.
type LinearHandles struct {
	W, Scales, Biases *Array
}

// Gemma4LayerConfig holds per-layer parameters.
type Gemma4LayerConfig struct {
	NumHeads       int
	NumKVHeads     int
	HeadDim        int
	SeqLen         int
	StartPos       int
	IsFull         bool
	HasPerLayer    bool
	HasLayerScalar bool
	RMSEps         float32
}

func b2i(b bool) int {
	if b {
		return 1
	} else {
		return 0
	}
}

// Gemma4KVLayer runs a full decoder layer with its own K/V projections in
// a single CGO call. Returns (output hidden state, new K cache, new V cache).
// newK/newV are nil if kCache was nil (first call, no existing cache).
func Gemma4KVLayer(
	h, perLayerIn, kCache, vCache, propRoPEFreqs *Array,
	cfg Gemma4LayerConfig,
	groupSize, bits int,
	inputNorm, postAttnNorm, preFFNorm, postFFNorm *Array,
	qProj, kProj, vProj, oProj LinearHandles,
	qNorm, kNorm *Array,
	gateProj, upProj, downProj LinearHandles,
	pligProj, plpProj LinearHandles,
	postPLINNorm, layerScalar *Array,
	s *Stream,
) (outH, newK, newV *Array, err error) {
	var outH_c, newK_c, newV_c C.mlx_array
	rc := C.gemma4_shim_kv_layer(
		&outH_c, &newK_c, &newV_c,
		h.cHandle(), cArr(perLayerIn), kCache.cHandle(), vCache.cHandle(),
		propRoPEFreqs.cHandle(),
		C.int(cfg.NumHeads), C.int(cfg.NumKVHeads), C.int(cfg.HeadDim),
		C.int(cfg.SeqLen), C.int(cfg.StartPos),
		C.int(b2i(cfg.IsFull)), C.int(b2i(cfg.HasPerLayer)), C.int(b2i(cfg.HasLayerScalar)),
		C.float(cfg.RMSEps),
		C.int(groupSize), C.int(bits),
		inputNorm.cHandle(), postAttnNorm.cHandle(), preFFNorm.cHandle(), postFFNorm.cHandle(),
		// Q proj + qnorm
		cArr(qProj.W), cArr(qProj.Scales), cArr(qProj.Biases), qNorm.cHandle(),
		// K proj + knorm
		cArr(kProj.W), cArr(kProj.Scales), cArr(kProj.Biases), kNorm.cHandle(),
		// V proj
		cArr(vProj.W), cArr(vProj.Scales), cArr(vProj.Biases),
		// O proj
		cArr(oProj.W), cArr(oProj.Scales), cArr(oProj.Biases),
		// MLP: gate, up, down
		cArr(gateProj.W), cArr(gateProj.Scales), cArr(gateProj.Biases),
		cArr(upProj.W), cArr(upProj.Scales), cArr(upProj.Biases),
		cArr(downProj.W), cArr(downProj.Scales), cArr(downProj.Biases),
		// Per-layer gating
		cArr(pligProj.W), cArr(pligProj.Scales), cArr(pligProj.Biases),
		cArr(plpProj.W), cArr(plpProj.Scales), cArr(plpProj.Biases),
		postPLINNorm.cHandle(),
		cArr(layerScalar),
		s.cHandle(),
	)
	if rc != 0 {
		return nil, nil, nil, fmt.Errorf("gemma4_kv_layer: %s", lastMLXError())
	}
	outH = wrap(outH_c)
	if newK_c.ctx != nil {
		newK = wrap(newK_c)
	}
	if newV_c.ctx != nil {
		newV = wrap(newV_c)
	}
	return outH, newK, newV, nil
}

// Gemma4SharedKVLayer runs a KV-shared decoder layer in a single CGO call.
func Gemma4SharedKVLayer(
	h, kForAttn, vForAttn, propRoPEFreqs, perLayerIn *Array,
	cfg Gemma4LayerConfig,
	groupSize, bits int,
	inputNorm, postAttnNorm, preFFNorm, postFFNorm *Array,
	qProj, oProj LinearHandles,
	qNorm *Array,
	gateProj, upProj, downProj LinearHandles,
	pligProj, plpProj LinearHandles,
	postPLINNorm, layerScalar *Array,
	s *Stream,
) (outH *Array, err error) {
	var outH_c C.mlx_array
	rc := C.gemma4_shim_shared_kv_layer(
		&outH_c,
		h.cHandle(), kForAttn.cHandle(), vForAttn.cHandle(),
		propRoPEFreqs.cHandle(), cArr(perLayerIn),
		C.int(cfg.NumHeads), C.int(cfg.NumKVHeads), C.int(cfg.HeadDim),
		C.int(cfg.SeqLen), C.int(cfg.StartPos),
		C.int(b2i(cfg.IsFull)), C.int(b2i(cfg.HasPerLayer)), C.int(b2i(cfg.HasLayerScalar)),
		C.float(cfg.RMSEps),
		C.int(groupSize), C.int(bits),
		inputNorm.cHandle(), postAttnNorm.cHandle(), preFFNorm.cHandle(), postFFNorm.cHandle(),
		// Q proj + qnorm
		cArr(qProj.W), cArr(qProj.Scales), cArr(qProj.Biases), qNorm.cHandle(),
		// O proj
		cArr(oProj.W), cArr(oProj.Scales), cArr(oProj.Biases),
		// MLP: gate, up, down
		cArr(gateProj.W), cArr(gateProj.Scales), cArr(gateProj.Biases),
		cArr(upProj.W), cArr(upProj.Scales), cArr(upProj.Biases),
		cArr(downProj.W), cArr(downProj.Scales), cArr(downProj.Biases),
		// Per-layer gating
		cArr(pligProj.W), cArr(pligProj.Scales), cArr(pligProj.Biases),
		cArr(plpProj.W), cArr(plpProj.Scales), cArr(plpProj.Biases),
		postPLINNorm.cHandle(),
		cArr(layerScalar),
		s.cHandle(),
	)
	if rc != 0 {
		return nil, fmt.Errorf("gemma4_shared_kv_layer: %s", lastMLXError())
	}
	return wrap(outH_c), nil
}

// Helpers to convert *Array to C.mlx_array (or MLX_NULL if nil).
func cArr(a *Array) C.mlx_array {
	if a != nil {
		return a.cHandle()
	}
	var z C.mlx_array
	return z
}
