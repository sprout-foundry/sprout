package embedding

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	onnxruntime "github.com/yalue/onnxruntime_go"
)

// TestCodeModelEval measures a candidate code-specific embedding model (Jina
// Code v2, q8 ONNX, 768-dim mean-pooled BERT) against the same regression
// corpus used to measure Gemma, so the two models can be compared
// apples-to-apples for the SP-135 Phase-0 gate: "beats Gemma's separation
// (near-dup-vs-unrelated gap; correct-hit floor) on ≥8/10 retrieval cases."
//
// The probe runs three tests:
//  1. Duplicate-detection separation (SumInts vs AddNumbers vs unrelated
//     filler) — Gemma's bands are 0.767/0.492/0.355 (near/related/unrelated).
//  2. NL-code retrieval (10 queries from retrieval_quality_test.go) — Gemma's
//     correct hits run 0.499–0.613, wrong answers ~0.32.
//  3. Throughput (units/s) — Gemma runs ~6.5 units/s q4 on M1 Pro.
//
// Opt-in: SPROUT_CODE_MODEL_EVAL=1. The model must be staged at
// ~/.local/share/sprout/models/jina-code-v2/ (model_q.onnx + tokenizer.json).
// Download: see SP-135 spec.
func TestCodeModelEval(t *testing.T) {
	if os.Getenv("SPROUT_CODE_MODEL_EVAL") != "1" {
		t.Skip("SPROUT_CODE_MODEL_EVAL unset")
	}

	ctx := context.Background()
	modelDir := DefaultModelDir()
	modelPath := filepath.Join(modelDir, "jina-code-v2", "model_quantized.onnx")
	tokenizerPath := filepath.Join(modelDir, "jina-code-v2", "tokenizer.json")
	if _, err := os.Stat(modelPath); err != nil {
		t.Skipf("jina-code-v2 model not staged at %s: %v", modelPath, err)
	}

	runtime, err := NewONNXRuntimeWithDir(modelDir)
	if err != nil {
		t.Skipf("ONNX runtime unavailable: %v", err)
	}
	defer runtime.Close()

	// Jina Code v2 uses a standard BERT ONNX export:
	//   inputs: input_ids [batch, seq], attention_mask [batch, seq]
	//   output: last_hidden_state [batch, seq, 768]
	// No pre-pooled sentence_embedding — pooling is done in Go (mean pool
	// with attention mask, then L2 normalize), matching Jina's reference
	// implementation.
	session, err := runtime.NewDynamicSession(modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"last_hidden_state"},
		SessionOption{IntraOpNumThreads: 4},
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer session.Destroy()

	// Jina's tokenizer is BPE with ByteLevel pre-tokenization (GPT-2 style).
	// The GemmaTokenizer (SentencePiece ▁ normalization) produces wrong token
	// IDs for Jina inputs. Use ByteLevelTokenizer instead.
	tok, err := NewByteLevelTokenizer(tokenizerPath)
	if err != nil {
		t.Fatalf("tokenizer: %v", err)
	}
	t.Logf("tokenizer: vocab_size=%d, bos=%d, eos=%d", tok.VocabSize(), tok.BOSID(), tok.EOSID())

	bosID := tok.BOSID()
	eosID := tok.EOSID()

	// Jina Code v2 is symmetric — it does NOT use task prefixes. The query
	// and document are embedded identically (raw text, no prefix prepended).
	// Gemma's "title: none | text: " and "task: code retrieval | query: "
	// prefixes are Gemma-specific and would corrupt Jina's embeddings.
	embed := func(text string) []float32 {
		t.Helper()
		tokenIDs := tok.Encode(text)
		if bosID >= 0 {
			tokenIDs = append([]int32{bosID}, tokenIDs...)
		}
		if eosID >= 0 {
			tokenIDs = append(tokenIDs, eosID)
		}
		if len(tokenIDs) > 2048 {
			tokenIDs = tokenIDs[:2048]
		}
		vec, err := runJinaInference(ctx, session, tokenIDs)
		if err != nil {
			t.Fatalf("inference: %v", err)
		}
		return vec
	}

	// ─── Test 1: Duplicate-detection separation ───
	t.Run("DuplicateSeparation", func(t *testing.T) {
		nearDup := "func SumInts(values []int) int {\n\ttotal := 0\n\tfor _, v := range values {\n\t\ttotal += v\n\t}\n\treturn total\n}"
		candidate := "func AddNumbers(nums []int) int {\n\tsum := 0\n\tfor _, n := range nums {\n\t\tsum += n\n\t}\n\treturn sum\n}"
		related := "func MaxInt(values []int) int {\n\tbest := values[0]\n\tfor _, v := range values[1:] {\n\t\tif v > best {\n\t\t\tbest = v\n\t\t}\n\t}\n\treturn best\n}"
		unrelated := "func Handler(name string, count int) string {\n\tif count <= 0 {\n\t\treturn \"none\"\n\t}\n\treturn name\n}"

		vNear := embed(nearDup)
		vCand := embed(candidate)
		vRel := embed(related)
		vUnrel := embed(unrelated)

		nearScore := CosineSimilarity(vNear, vCand)
		relScore := CosineSimilarity(vNear, vRel)
		unrelScore := CosineSimilarity(vNear, vUnrel)

		t.Logf("CODE MODEL duplicate-detection bands:")
		t.Logf("  near-duplicate : %.4f  (Gemma q4: 0.767)", nearScore)
		t.Logf("  related        : %.4f  (Gemma q4: 0.492)", relScore)
		t.Logf("  unrelated      : %.4f  (Gemma q4: 0.355)", unrelScore)
		t.Logf("  separation gap : %.4f  (Gemma q4: 0.412)", nearScore-unrelScore)

		// Gate: separation gap must be ≥ Gemma's 0.412
		if gap := nearScore - unrelScore; gap < 0.41 {
			t.Errorf("CODE MODEL separation gap %.4f < Gemma's 0.412 — code model is worse at duplicate detection", gap)
		}
	})

	// ─── Test 2: NL-code retrieval (10 queries) ───
	t.Run("NLCodeRetrieval", func(t *testing.T) {
		// Same corpus as retrieval_quality_test.go.
		queries := []struct {
			query   string
			correct string
			wrong   string
		}{
			{"sort an array of integers in place",
				"func sortInts(arr []int) {\n\tfor i := 0; i < len(arr); i++ {\n\t\tfor j := i + 1; j < len(arr); j++ {\n\t\t\tif arr[i] > arr[j] {\n\t\t\t\tarr[i], arr[j] = arr[j], arr[i]\n\t\t\t}\n\t\t}\n\t}\n}",
				"func readFile(path string) (string, error) {\n\tdata, err := os.ReadFile(path)\n\treturn string(data), err\n}"},
			{"read a file and return its lines as a slice",
				"func readFileLines(path string) ([]string, error) {\n\tdata, err := os.ReadFile(path)\n\tlines := strings.Split(string(data), \"\\n\")\n\treturn lines, err\n}",
				"func computeHash(data string) string {\n\th := sha256.Sum256([]byte(data))\n\treturn hex.EncodeToString(h[:])\n}"},
			{"parse a JSON string into a struct",
				"func parseJSON(data string, v interface{}) error {\n\treturn json.Unmarshal([]byte(data), v)\n}",
				"func retryRequest(req *http.Request) (*http.Response, error) {\n\tfor i := 0; i < 3; i++ {\n\t\tresp, err := http.DefaultClient.Do(req)\n\t\tif err == nil { return resp, nil }\n\t}\n\treturn nil, errors.New(\"retries exhausted\")\n}"},
			{"compute the sha256 hash of a string",
				"func sha256Hash(s string) string {\n\th := sha256.Sum256([]byte(s))\n\treturn hex.EncodeToString(h[:])\n}",
				"func watchDir(dir string) { fsnotify.NewWatcher() }"},
			{"find all callers of a function in the codegraph",
				"func GetCallers(fn string) []Edge {\n\treturn store.QueryEdgesByTarget(fn)\n}",
				"func SumInts(values []int) int {\n\ttotal := 0\n\treturn total\n}"},
			{"retry a network request with exponential backoff",
				"func retryWithBackoff(req *http.Request, maxRetries int) (*http.Response, error) {\n\tfor i := 0; i < maxRetries; i++ {\n\t\tresp, err := http.DefaultClient.Do(req)\n\t\tif err == nil { return resp, nil }\n\t\ttime.Sleep(time.Duration(1<<i) * time.Second)\n\t}\n\treturn nil, err\n}",
				"func parseJSON(data string) { json.Unmarshal([]byte(data)) }"},
			{"watch a directory for file changes",
				"func watchDir(dir string, handler func(string)) {\n\twatcher, _ := fsnotify.NewWatcher()\n\twatcher.Add(dir)\n}",
				"func sha256Hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }"},
			{"merge two sorted slices without duplicates",
				"func mergeSorted(a, b []int) []int {\n\tseen := map[int]bool{}\n\tvar result []int\n\tfor _, v := range append(a, b...) {\n\t\tif !seen[v] { result = append(result, v); seen[v] = true }\n\t}\n\treturn result\n}",
				"func readFile(path string) { os.ReadFile(path) }"},
			{"encode a struct to JSON bytes",
				"func toJSON(v interface{}) ([]byte, error) {\n\treturn json.Marshal(v)\n}",
				"func sortInts(arr []int) {\n\tsort.Ints(arr)\n}"},
			{"check whether a shell command is safe to auto-run",
				"func classifyCommand(cmd string) RiskLevel {\n\treturn classifier.Classify(cmd)\n}",
				"func readFile(path string) (string, error) {\n\tdata, _ := os.ReadFile(path)\n\treturn string(data), nil\n}"},
		}

		passes := 0
		var scores []float64
		for _, tc := range queries {
			qv := embed(tc.query)
			cv := embed(tc.correct)
			wv := embed(tc.wrong)
			correctScore := float64(CosineSimilarity(qv, cv))
			wrongScore := float64(CosineSimilarity(qv, wv))
			passed := correctScore > wrongScore
			if passed {
				passes++
			}
			scores = append(scores, correctScore)
			t.Logf("  %s: correct=%.3f wrong=%.3f %s",
				tc.query[:min(40, len(tc.query))], correctScore, wrongScore,
				map[bool]string{true: "✓", false: "✗"}[passed])
		}

		sort.Float64s(scores)
		t.Logf("CODE MODEL NL-code retrieval: %d/10 correct (Gemma: 10/10)", passes)
		t.Logf("  correct-hit range: %.3f–%.3f (Gemma: 0.499–0.613)", scores[0], scores[len(scores)-1])

		if passes < 8 {
			t.Errorf("CODE MODEL retrieval: %d/10 < gate 8/10 — code model fails the retrieval gate", passes)
		}
	})

	// ─── Test 3: Throughput ───
	t.Run("Throughput", func(t *testing.T) {
		sample := "func processItem(item *Item) error {\n\tif item == nil {\n\t\treturn fmt.Errorf(\"nil item\")\n\t}\n\titem.processed = true\n\titem.timestamp = time.Now()\n\treturn nil\n}"
		tokenIDs := append([]int32{bosID}, tok.Encode(sample)...)
		tokenIDs = append(tokenIDs, eosID)

		N := 100
		start := time.Now()
		for i := 0; i < N; i++ {
			_, err := runJinaInference(ctx, session, tokenIDs)
			if err != nil {
				t.Fatalf("throughput inference %d: %v", i, err)
			}
		}
		elapsed := time.Since(start)
		rate := float64(N) / elapsed.Seconds()
		perUnit := elapsed / time.Duration(N)
		t.Logf("CODE MODEL throughput: %d units in %s (%.1f units/s, %s/unit)",
			N, elapsed.Round(time.Millisecond), rate, perUnit.Round(time.Microsecond))
		t.Logf("  (Gemma q4: ~6.5 units/s)")
	})
}

// runJinaInference runs a single ONNX inference call against the Jina Code v2
// model and returns an L2-normalized, mean-pooled 768-dim vector.
//
// Unlike Gemma (which exports a pre-pooled sentence_embedding output), Jina's
// ONNX graph outputs last_hidden_state [batch, seq_len, 768]. Mean pooling
// with the attention mask is done here, matching Jina's reference impl.
func runJinaInference(ctx context.Context, session *onnxruntime.DynamicAdvancedSession, tokenIDs []int32) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	batchSize := int64(1)
	seqLen := int64(len(tokenIDs))
	hiddenDim := int64(768)

	inputIDs := make([]int64, len(tokenIDs))
	attentionMask := make([]int64, len(tokenIDs))
	for i, id := range tokenIDs {
		inputIDs[i] = int64(id)
		attentionMask[i] = 1
	}

	inputIDsTensor, err := onnxruntime.NewTensor(onnxruntime.NewShape(batchSize, seqLen), inputIDs)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer inputIDsTensor.Destroy()

	attnMaskTensor, err := onnxruntime.NewTensor(onnxruntime.NewShape(batchSize, seqLen), attentionMask)
	if err != nil {
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer attnMaskTensor.Destroy()

	// Output is [batch, seq_len, 768] — full hidden states, not pooled.
	outputTensor, err := onnxruntime.NewEmptyTensor[float32](onnxruntime.NewShape(batchSize, seqLen, hiddenDim))
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	if err := session.Run(
		[]onnxruntime.Value{inputIDsTensor, attnMaskTensor},
		[]onnxruntime.Value{outputTensor},
	); err != nil {
		return nil, fmt.Errorf("run inference: %w", err)
	}

	hidden := outputTensor.GetData()
	if int64(len(hidden)) < batchSize*seqLen*hiddenDim {
		return nil, fmt.Errorf("last_hidden_state returned %d floats, expected %d", len(hidden), batchSize*seqLen*hiddenDim)
	}

	// Mean pool over sequence dimension with attention mask (all ones for
	// unpadded single-sequence inference).
	pooled := make([]float32, hiddenDim)
	for i := int64(0); i < seqLen; i++ {
		offset := i * hiddenDim
		for j := int64(0); j < hiddenDim; j++ {
			pooled[j] += hidden[offset+j]
		}
	}
	invSeq := float32(1.0 / float64(seqLen))
	for j := range pooled {
		pooled[j] *= invSeq
	}

	// L2 normalize.
	var norm float32
	for _, v := range pooled {
		norm += v * v
	}
	if norm > 1e-9 {
		inv := float32(1.0 / math.Sqrt(float64(norm)))
		for j := range pooled {
			pooled[j] *= inv
		}
	}

	return pooled, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
