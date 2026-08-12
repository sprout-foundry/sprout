//go:build (darwin || linux) && arm64 && cgo && (mlx || ggml)

package qwen35

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// mtpWeights holds the multi-token prediction (MTP) module. Qwen3.5 models
// ship an optional 1-layer "draft" head: given the main model's hidden state
// at position t and the token at position t+1, it predicts the token at t+2
// (DeepSeek-V3 style). The module is:
//
//	e' = pre_fc_norm_embedding(embed(next_token))
//	h' = pre_fc_norm_hidden(prev_hidden)
//	x  = fc(concat([e', h'], dim=-1))   // [2H] -> [H]
//	x  = layers.0(x)                     // one decoder layer (full attention)
//	logits = head(norm(x))               // shared lm_head
//
// The head is the main model's lm_head (tied embeddings). MTP weights are
// present only in raw-HF exports (mlx-community conversions strip them); the
// module is a no-op (nil) on models without them.
type mtpWeights struct {
	preFCHiddenNorm    tensor.Array // [hidden] RMSNorm on prev hidden
	preFCEmbeddingNorm tensor.Array // [hidden] RMSNorm on next-token embedding
	fc                 *llm.Linear  // [hidden, 2*hidden]
	layer              layerWeights // one full-attention decoder layer
	norm               tensor.Array // [hidden] final RMSNorm before head
}

// hasMTPWeights reports whether the safetensors file contains MTP tensors.
func hasMTPWeights(sf *llm.SafetensorsFile) bool {
	return sf.Has("mtp.pre_fc_norm_hidden.weight") ||
		sf.Has("mtp.pre_fc_norm_embedding.weight") ||
		sf.Has("mtp.fc.weight")
}

// loadMTPWeights loads the MTP module from the top-level `mtp.` namespace.
// Returns (nil, nil) when the file has no MTP tensors.
func loadMTPWeights(sf *llm.SafetensorsFile, backend tensor.Backend, s tensor.Stream, quant *llm.QuantConfig) (*mtpWeights, error) {
	if !hasMTPWeights(sf) {
		return nil, nil
	}
	m := &mtpWeights{}
	var err error

	m.preFCHiddenNorm, err = sf.Get("mtp.pre_fc_norm_hidden.weight", backend, s)
	if err != nil {
		return nil, fmt.Errorf("mtp pre_fc_norm_hidden: %w", err)
	}
	m.preFCEmbeddingNorm, err = sf.Get("mtp.pre_fc_norm_embedding.weight", backend, s)
	if err != nil {
		return nil, fmt.Errorf("mtp pre_fc_norm_embedding: %w", err)
	}
	// fc is [H, 2H]. Raw-HF exports carry it as BF16; a quantized export
	// would carry the packed triplet — respect the model's quant config.
	m.fc, err = llm.LoadLinear(sf, "mtp.fc.weight", backend, s, quant)
	if err != nil {
		return nil, fmt.Errorf("mtp fc: %w", err)
	}
	m.norm, err = sf.Get("mtp.norm.weight", backend, s)
	if err != nil {
		return nil, fmt.Errorf("mtp norm: %w", err)
	}

	// The MTP block is a single full-attention decoder layer.
	p := "mtp.layers.0"
	lw := &m.layer
	lw.inputNorm, err = sf.Get(p+".input_layernorm.weight", backend, s)
	if err != nil {
		return nil, fmt.Errorf("mtp layer input norm: %w", err)
	}
	lw.postNorm, err = sf.Get(p+".post_attention_layernorm.weight", backend, s)
	if err != nil {
		return nil, fmt.Errorf("mtp layer post norm: %w", err)
	}
	sa := &selfAttnWeights{}
	sa.qProj, err = llm.LoadLinear(sf, p+".self_attn.q_proj.weight", backend, s, quant)
	if err != nil {
		return nil, fmt.Errorf("mtp q_proj: %w", err)
	}
	sa.kProj, err = llm.LoadLinear(sf, p+".self_attn.k_proj.weight", backend, s, quant)
	if err != nil {
		return nil, fmt.Errorf("mtp k_proj: %w", err)
	}
	sa.vProj, err = llm.LoadLinear(sf, p+".self_attn.v_proj.weight", backend, s, quant)
	if err != nil {
		return nil, fmt.Errorf("mtp v_proj: %w", err)
	}
	sa.oProj, err = llm.LoadLinear(sf, p+".self_attn.o_proj.weight", backend, s, quant)
	if err != nil {
		return nil, fmt.Errorf("mtp o_proj: %w", err)
	}
	sa.qNorm, err = sf.Get(p+".self_attn.q_norm.weight", backend, s)
	if err != nil {
		return nil, fmt.Errorf("mtp q_norm: %w", err)
	}
	sa.kNorm, err = sf.Get(p+".self_attn.k_norm.weight", backend, s)
	if err != nil {
		return nil, fmt.Errorf("mtp k_norm: %w", err)
	}
	lw.selfAttn = sa

	lw.gateProj, err = llm.LoadLinear(sf, p+".mlp.gate_proj.weight", backend, s, quant)
	if err != nil {
		return nil, fmt.Errorf("mtp gate_proj: %w", err)
	}
	lw.upProj, err = llm.LoadLinear(sf, p+".mlp.up_proj.weight", backend, s, quant)
	if err != nil {
		return nil, fmt.Errorf("mtp up_proj: %w", err)
	}
	lw.downProj, err = llm.LoadLinear(sf, p+".mlp.down_proj.weight", backend, s, quant)
	if err != nil {
		return nil, fmt.Errorf("mtp down_proj: %w", err)
	}
	return m, nil
}

// Free releases all MLX arrays held by the MTP module.
func (m *mtpWeights) Free() {
	if m == nil {
		return
	}
	freeArr(m.preFCHiddenNorm)
	freeArr(m.preFCEmbeddingNorm)
	freeArr(m.norm)
	if m.fc != nil {
		m.fc.Free()
	}
	lw := &m.layer
	if lw.selfAttn != nil {
		freeArr(lw.selfAttn.qNorm)
		freeArr(lw.selfAttn.kNorm)
		if lw.selfAttn.qProj != nil {
			lw.selfAttn.qProj.Free()
		}
		if lw.selfAttn.kProj != nil {
			lw.selfAttn.kProj.Free()
		}
		if lw.selfAttn.vProj != nil {
			lw.selfAttn.vProj.Free()
		}
		if lw.selfAttn.oProj != nil {
			lw.selfAttn.oProj.Free()
		}
	}
	if lw.gateProj != nil {
		lw.gateProj.Free()
	}
	if lw.upProj != nil {
		lw.upProj.Free()
	}
	if lw.downProj != nil {
		lw.downProj.Free()
	}
}

// forward runs the MTP module for ONE position.
//
//	prevHidden: [1, 1, H] main model hidden state at position t
//	nextEmb:    [1, 1, H] embedding of the token at position t+1
//
// Returns logits [1, 1, vocab] predicting the token at position t+2.
// The MTP decoder layer attends over a single position with no KV history.
func (m *mtpWeights) forward(q *Qwen35, prevHidden, nextEmb tensor.Array) (tensor.Array, error) {
	logits, _, err := m.forwardStep(q, prevHidden, nextEmb)
	if err != nil {
		return nil, err
	}
	return logits, nil
}

// draftChain runs K MTP steps, chaining each step's decoder-layer output as
// the next step's prevHidden (DeepSeek-V3 self-referential drafting).
//
//	prevHidden: main model hidden state at position t
//	nextToken:  token at position t+1
//	k:          number of drafts to produce
//
// Returns K draft token IDs predicting positions t+2..t+K+1. All
// intermediate MLX arrays are freed.
func (m *mtpWeights) draftChain(q *Qwen35, prevHidden tensor.Array, nextToken int, k int) ([]int, error) {
	drafts := make([]int, 0, k)
	curHidden := prevHidden // borrowed — owned by caller
	curTok := nextToken

	for i := 0; i < k; i++ {
		emb, err := q.weights.embed.Lookup(q.makeOneTokenArray(curTok), q.backend, q.stream)
		if err != nil {
			return nil, fmt.Errorf("mtp draft %d lookup: %w", i, err)
		}
		// GatherAxis adds the indices' shape as leading dims, so a [1,1] ids
		// array yields [1,1,1,H] — squeeze axis 2 to match prevHidden [1,1,H].
		emb, err = q.backend.SqueezeAxis(emb, 2, q.stream)
		if err != nil {
			emb.Free()
			return nil, fmt.Errorf("mtp draft %d squeeze: %w", i, err)
		}
		logits, layerOut, err := m.forwardStep(q, curHidden, emb)
		emb.Free()
		if err != nil {
			return nil, fmt.Errorf("mtp draft %d: %w", i, err)
		}

		draft, err := q.logitsToArgmax(logits)
		logits.Free()
		if err != nil {
			if layerOut != nil {
				layerOut.Free()
			}
			return nil, fmt.Errorf("mtp draft %d argmax: %w", i, err)
		}

		drafts = append(drafts, draft)

		// Chain: next step's prevHidden is this step's decoder layer output.
		if curHidden != prevHidden && curHidden != nil {
			curHidden.Free()
		}
		curHidden = layerOut
		curTok = draft
	}

	if curHidden != prevHidden && curHidden != nil {
		curHidden.Free()
	}
	return drafts, nil
}

// forwardStep runs the MTP module for ONE position and returns both the
// logits [1, 1, vocab] and the decoder layer's output hidden state
// [1, 1, H]. The hidden state is used to chain drafts: the next MTP step
// feeds the previous step's layer output as prevHidden (DeepSeek-V3 style
// self-referential drafting). Caller frees both.
func (m *mtpWeights) forwardStep(q *Qwen35, prevHidden, nextEmb tensor.Array) (tensor.Array, tensor.Array, error) {
	s := q.stream

	// e' = pre_fc_norm_embedding(next_emb)
	eNorm, err := q.rmsNormQwen35(nextEmb, m.preFCEmbeddingNorm)
	if err != nil {
		return nil, nil, fmt.Errorf("mtp embed norm: %w", err)
	}
	defer eNorm.Free()

	// h' = pre_fc_norm_hidden(prev_hidden)
	hNorm, err := q.rmsNormQwen35(prevHidden, m.preFCHiddenNorm)
	if err != nil {
		return nil, nil, fmt.Errorf("mtp hidden norm: %w", err)
	}
	defer hNorm.Free()

	// x = fc(concat([e', h'], dim=-1))
	cat, err := q.backend.ConcatenateAxis([]tensor.Array{eNorm, hNorm}, -1, s)
	if err != nil {
		return nil, nil, fmt.Errorf("mtp concat: %w", err)
	}
	defer cat.Free()

	x, err := m.fc.Forward(cat, q.backend, s)
	if err != nil {
		return nil, nil, fmt.Errorf("mtp fc: %w", err)
	}
	defer x.Free()

	// one full-attention decoder layer (single position, no KV cache).
	normed, err := q.rmsNormQwen35(x, m.layer.inputNorm)
	if err != nil {
		return nil, nil, fmt.Errorf("mtp layer input norm: %w", err)
	}
	defer normed.Free()

	attnOut, err := q.fullAttention(normed, &m.layer, 0, 1, 0, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("mtp attention: %w", err)
	}
	defer attnOut.Free()

	res1, err := q.backend.Add(x, attnOut, s)
	if err != nil {
		return nil, nil, fmt.Errorf("mtp attn residual: %w", err)
	}
	defer res1.Free()

	normed2, err := q.rmsNormQwen35(res1, m.layer.postNorm)
	if err != nil {
		return nil, nil, fmt.Errorf("mtp post norm: %w", err)
	}
	defer normed2.Free()

	ffnOut, err := q.swiglu(normed2, &m.layer)
	if err != nil {
		return nil, nil, fmt.Errorf("mtp ffn: %w", err)
	}
	defer ffnOut.Free()

	layerOut, err := q.backend.Add(res1, ffnOut, s)
	if err != nil {
		return nil, nil, fmt.Errorf("mtp layer residual: %w", err)
	}

	normed3, err := q.rmsNormQwen35(layerOut, m.norm)
	if err != nil {
		layerOut.Free()
		return nil, nil, fmt.Errorf("mtp final norm: %w", err)
	}
	defer normed3.Free()

	logits, err := q.headOnlyLogits(normed3)
	if err != nil {
		layerOut.Free()
		return nil, nil, fmt.Errorf("mtp head: %w", err)
	}
	return logits, layerOut, nil
}
