//go:build darwin && arm64 && cgo && mlx

package embedding

import (
	"fmt"
	"math"

	"github.com/sprout-foundry/sprout/pkg/mlx"
)

// forward runs the full transformer forward pass: embeddings → 12 layers →
// output. Returns the last_hidden_state [batch, seq, hidden].
// attnMask is an additive bias [batch, 1, 1, seq] for padding (0 for real
// tokens, -1e9 for padding), or nil when there is no padding (single-seq).
func (p *MLXEmbeddingProvider) forward(ids *mlx.Array, seq int, attnMask *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	w := p.weights

	// ── Embeddings ──
	// Gather word embeddings by token ID.
	// ids is [batch, seq] int64 → gather rows from wordEmb [vocab, hidden].
	// Result: [batch, seq, hidden]
	wordEmb, err := embeddingLookup(w.wordEmb, ids, s)
	if err != nil {
		return nil, fmt.Errorf("word embedding: %w", err)
	}

	// Add token-type embeddings. All tokens have token_type=0, so gather row 0
	// from tokEmb [type_vocab_size, hidden] and broadcast over [batch, seq, hidden].
	tokTypeIdx, err := mlx.NewArrayFromInt64([]int64{0}, []int{1})
	if err != nil {
		wordEmb.Free()
		return nil, fmt.Errorf("create tok type idx: %w", err)
	}
	defer tokTypeIdx.Free()

	// GatherAxis on [vocab_types, hidden] with idx [0] → [1, 1, hidden]
	tokType, err := mlx.GatherAxis(w.tokEmb, tokTypeIdx, 0, []int{1, jinaHidden}, s)
	if err != nil {
		wordEmb.Free()
		return nil, fmt.Errorf("gather tok type: %w", err)
	}
	defer tokType.Free()

	embInput, err := mlx.Add(wordEmb, tokType, s)
	if err != nil {
		wordEmb.Free()
		return nil, fmt.Errorf("add tok type: %w", err)
	}
	wordEmb.Free()

	// Embedding LayerNorm
	h, err := layerNorm(embInput, w.embNormW, w.embNormB, jinaEps, s)
	if err != nil {
		embInput.Free()
		return nil, fmt.Errorf("embed layernorm: %w", err)
	}
	embInput.Free()

	// ── ALiBi bias ──
	alibi, err := buildALiBiBias(jinaHeads, seq, s)
	if err != nil {
		h.Free()
		return nil, fmt.Errorf("alibi: %w", err)
	}
	defer alibi.Free()

	// ── Transformer layers ──
	for i := 0; i < numJinaLayers; i++ {
		out, err := jinaForwardLayer(h, w.layers[i], alibi, attnMask, jinaHeads, seq, s)
		if err != nil {
			h.Free()
			return nil, fmt.Errorf("layer %d: %w", i, err)
		}
		h.Free()
		h = out
	}

	return h, nil
}

// jinaForwardLayer runs one Jina BERT layer:
//
//	residual = h
//	attn_out = LayerNorm(dense(attention(QK-LN(h))) + h)
//	residual = LayerNorm_1(h + attn_out)
//	ffn = down(up * GELU(gate))
//	out = LayerNorm_2(residual + ffn)
//
// attnMask is an additive padding bias [batch, 1, 1, seq] or nil.
func jinaForwardLayer(h *mlx.Array, lw *jinaLayerWeights, alibi, attnMask *mlx.Array, heads, seq int, s *mlx.Stream) (*mlx.Array, error) {
	hShape := h.Shape()
	batch := hShape[0]

	// ── Attention ──
	// Q = layer_norm_q(query(h))  → [batch, seq, hidden]
	// K = layer_norm_k(key(h))    → [batch, seq, hidden]
	// V = value(h)                → [batch, seq, hidden]
	qProj, err := linear(h, lw.qProjW, lw.qProjB, s)
	if err != nil {
		return nil, fmt.Errorf("q proj: %w", err)
	}
	defer qProj.Free()

	q, err := layerNorm(qProj, lw.qLnW, lw.qLnB, jinaEps, s)
	if err != nil {
		qProj.Free()
		return nil, fmt.Errorf("q layernorm: %w", err)
	}
	qProj.Free()

	kProj, err := linear(h, lw.kProjW, lw.kProjB, s)
	if err != nil {
		return nil, fmt.Errorf("k proj: %w", err)
	}
	defer kProj.Free()

	k, err := layerNorm(kProj, lw.kLnW, lw.kLnB, jinaEps, s)
	if err != nil {
		kProj.Free()
		return nil, fmt.Errorf("k layernorm: %w", err)
	}
	kProj.Free()

	v, err := linear(h, lw.vProjW, lw.vProjB, s)
	if err != nil {
		return nil, fmt.Errorf("v proj: %w", err)
	}
	defer v.Free()

	// Reshape to [batch, seq, heads, headDim] then transpose to [batch, heads, seq, headDim]
	q4, err := reshapeHeads(q, batch, seq, heads, s)
	if err != nil {
		return nil, err
	}
	defer q4.Free()

	k4, err := reshapeHeads(k, batch, seq, heads, s)
	if err != nil {
		return nil, err
	}
	defer k4.Free()

	v4, err := reshapeHeads(v, batch, seq, heads, s)
	if err != nil {
		return nil, err
	}
	defer v4.Free()

	// scores = Q @ K^T / sqrt(headDim) + alibi
	// K^T: transpose last two dims of [batch, heads, seq, headDim]
	kT, err := mlx.TransposeAxes(k4, []int{0, 1, 3, 2}, s)
	if err != nil {
		return nil, fmt.Errorf("k transpose: %w", err)
	}
	defer kT.Free()

	scores, err := mlx.MatMul(q4, kT, s)
	if err != nil {
		return nil, fmt.Errorf("attention scores: %w", err)
	}
	defer scores.Free()

	// Scale by 1/sqrt(headDim)
	scale := float32(1.0 / math.Sqrt(float64(jinaHeadDim)))
	scaleArr, err := mlx.NewArrayFromFloat32([]float32{scale}, []int{1})
	if err != nil {
		return nil, err
	}
	defer scaleArr.Free()

	scaled, err := mlx.Multiply(scores, scaleArr, s)
	if err != nil {
		return nil, fmt.Errorf("scale scores: %w", err)
	}
	defer scaled.Free()

	// Add ALiBi bias
	alibiAdjusted, err := mlx.Add(scaled, alibi, s)
	if err != nil {
		return nil, fmt.Errorf("add alibi: %w", err)
	}
	defer alibiAdjusted.Free()

	// Add padding mask (additive bias) when present: [batch, 1, 1, seq] broadcasts
	scoreInput := alibiAdjusted
	if attnMask != nil {
		maskedScores, err := mlx.Add(alibiAdjusted, attnMask, s)
		if err != nil {
			return nil, fmt.Errorf("add attn mask: %w", err)
		}
		defer maskedScores.Free()
		scoreInput = maskedScores
	}

	// Softmax along the last axis (key dimension). SoftmaxAxis is required
	// here because the axis-less Softmax flattens the whole array.
	probs, err := mlx.SoftmaxAxis(scoreInput, -1, s)
	if err != nil {
		return nil, fmt.Errorf("softmax: %w", err)
	}
	defer probs.Free()

	// context = probs @ V → [batch, heads, seq, headDim]
	ctx2, err := mlx.MatMul(probs, v4, s)
	if err != nil {
		return nil, fmt.Errorf("attention context: %w", err)
	}
	defer ctx2.Free()

	// Transpose [batch, heads, seq, headDim] → [batch, seq, heads, headDim]
	ctx3, err := mlx.TransposeAxes(ctx2, []int{0, 2, 1, 3}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx transpose: %w", err)
	}
	defer ctx3.Free()

	attnOut, err := mlx.Reshape(ctx3, []int{batch, seq, jinaHidden}, s)
	if err != nil {
		return nil, fmt.Errorf("ctx reshape (ctx3 shape=%v, batch=%d, seq=%d): %w", ctx3.Shape(), batch, seq, err)
	}
	defer attnOut.Free()

	// Output projection
	denseOut, err := linear(attnOut, lw.outProjW, lw.outProjB, s)
	if err != nil {
		return nil, fmt.Errorf("out proj: %w", err)
	}
	defer denseOut.Free()

	// attn_layernorm = LayerNorm(denseOut + h)
	residual, err := mlx.Add(denseOut, h, s)
	if err != nil {
		return nil, fmt.Errorf("attn residual: %w", err)
	}
	defer residual.Free()

	attnNormed, err := layerNorm(residual, lw.attnLnW, lw.attnLnB, jinaEps, s)
	if err != nil {
		return nil, fmt.Errorf("attn layernorm: %w", err)
	}
	defer attnNormed.Free()

	// merge = LayerNorm_1(h + attnNormed)
	mergeIn, err := mlx.Add(h, attnNormed, s)
	if err != nil {
		return nil, fmt.Errorf("merge add: %w", err)
	}
	defer mergeIn.Free()

	merged, err := layerNorm(mergeIn, lw.ln1W, lw.ln1B, jinaEps, s)
	if err != nil {
		return nil, fmt.Errorf("ln1: %w", err)
	}
	defer merged.Free()

	// GEGLU FFN: down(up * GELU(gate))
	gateUp, err := linear(merged, lw.gateUpW, nil, s)
	if err != nil {
		return nil, fmt.Errorf("gate_up: %w", err)
	}
	defer gateUp.Free()

	// splitLastDim returns (up=first_half, gate=second_half).
	upPart, gatePart, err := splitLastDim(gateUp, jinaIntermediate, s)
	if err != nil {
		return nil, fmt.Errorf("split geglu: %w", err)
	}
	defer gatePart.Free()
	defer upPart.Free()

	geluGate, err := gelu(gatePart, s)
	if err != nil {
		return nil, fmt.Errorf("gelu: %w", err)
	}
	defer geluGate.Free()

	gated, err := mlx.Multiply(upPart, geluGate, s)
	if err != nil {
		return nil, fmt.Errorf("geglu multiply: %w", err)
	}
	defer gated.Free()

	ffnOut, err := linear(gated, lw.downW, lw.downB, s)
	if err != nil {
		return nil, fmt.Errorf("down proj: %w", err)
	}
	defer ffnOut.Free()

	// out = LayerNorm_2(merged + ffnOut)
	ffnResidual, err := mlx.Add(merged, ffnOut, s)
	if err != nil {
		return nil, fmt.Errorf("ffn residual: %w", err)
	}
	defer ffnResidual.Free()

	return layerNorm(ffnResidual, lw.ln2W, lw.ln2B, jinaEps, s)
}

// reshapeHeads reshapes [batch, seq, hidden] → [batch, heads, seq, headDim].
func reshapeHeads(x *mlx.Array, batch, seq, heads int, s *mlx.Stream) (*mlx.Array, error) {
	r1, err := mlx.Reshape(x, []int{batch, seq, heads, jinaHeadDim}, s)
	if err != nil {
		return nil, err
	}
	defer r1.Free()

	return mlx.TransposeAxes(r1, []int{0, 2, 1, 3}, s)
}
