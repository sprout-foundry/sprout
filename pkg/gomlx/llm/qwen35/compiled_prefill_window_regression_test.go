//go:build darwin && arm64 && cgo

package qwen35

import (
	"os"
	"runtime"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// TestCompiledDecodePrefillWindowRegression guards the prefill-staging half
// of compiled decode. copyPrefillWindow builds the fixed-capacity K/V
// buffers via SliceUpdate, which is FUNCTIONAL in MLX: it returns new
// arrays and never writes the zero-padded originals in place. A past bug
// freed the SliceUpdate results and kept the all-zero originals — the 8
// full-attention layers then attended to nothing from the prompt, which
// still emitted plausible text (24 of 32 layers are DeltaNet and carried
// the recurrence), so only a buffer readback catches it. After
// PrepareCompiledDecode on a real prefilled prompt, every full-attention
// layer's K buffer must contain the prefilled window (non-zero values).
func TestCompiledDecodePrefillWindowRegression(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	backend := &mlx.MetalBackend{}
	stream, err := backend.NewGPUStream()
	if err != nil {
		t.Fatalf("NewGPUStream: %v", err)
	}
	defer stream.Free()

	cfg, err := llm.LoadConfig(dir + "/config.json")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	arch, err := New(cfg, backend)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	q := arch.(*Qwen35)
	q.SetStream(stream)
	if err := q.InitWeights(dir+"/model.safetensors", stream); err != nil {
		t.Fatalf("InitWeights: %v", err)
	}

	tok, err := llm.LoadTokenizer(dir + "/tokenizer.json")
	if err != nil {
		t.Fatalf("LoadTokenizer: %v", err)
	}
	prompt := "The capital of France is"
	ids := tok.Encode(prompt)
	if len(ids) == 0 {
		t.Fatal("tokenizer produced no ids")
	}

	cache := llm.NewKVCache(cfg.NumLayers, stream, backend)
	idData := make([]int64, len(ids))
	for i, id := range ids {
		idData[i] = int64(id)
	}
	idArr, err := backend.NewArrayFromInt64(idData, []int{1, len(ids)})
	if err != nil {
		t.Fatalf("ids array: %v", err)
	}
	if _, err := q.ForwardPrefill(idArr, len(ids), cache); err != nil {
		t.Fatalf("ForwardPrefill: %v", err)
	}

	if err := q.PrepareCompiledDecode(len(ids), 16, cache); err != nil {
		t.Fatalf("PrepareCompiledDecode: %v", err)
	}
	defer q.ReleaseCompiledDecode()

	checked := 0
	for i := 0; i < cfg.NumLayers; i++ {
		if q.cd == nil || q.cd.kBufs[i] == nil {
			continue
		}
		kf, err := mlx.AsType(q.cd.kBufs[i].(*mlx.Array), mlx.Float32, stream.(*mlx.Stream))
		if err != nil {
			t.Fatalf("layer %d AsType: %v", i, err)
		}
		abs, err := mlx.Abs(kf, stream.(*mlx.Stream))
		kf.Free()
		if err != nil {
			t.Fatalf("layer %d Abs: %v", i, err)
		}
		if err := abs.Eval(); err != nil {
			abs.Free()
			t.Fatalf("layer %d eval: %v", i, err)
		}
		data, err := abs.Float32Data()
		abs.Free()
		if err != nil {
			t.Fatalf("layer %d readback: %v", i, err)
		}
		nonZero := 0
		for _, v := range data {
			if v != 0 {
				nonZero++
			}
		}
		if nonZero == 0 {
			t.Fatalf("layer %d K buffer is all zeros — copyPrefillWindow regression: SliceUpdate result was discarded", i)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no full-attention layers found — model layout unexpected")
	}
	t.Logf("%d full-attention layers carry prefilled K windows", checked)
}
