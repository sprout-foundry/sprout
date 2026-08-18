//go:build darwin && arm64 && cgo

package llm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestGemmaTokenizerParity compares the Go tokenizer's Encode/Decode against
// HuggingFace tokenizers ground truth for the stock gemma-4 e2b model.
// Ground-truth IDs were produced with:
//
//	python3 -c "from tokenizers import Tokenizer; \
//	  t=Tokenizer.from_file('.../tokenizer.json'); print(t.encode(s).ids)"
type parityCase struct {
	name string
	text string
	want []int // HF ground truth; nil skips the encode assertion
}

var parityCases = []parityCase{
	{"plain", "hello world", []int{23391, 1902}},
	{"capitals", "Hello, my name is", []int{9259, 236764, 1041, 1463, 563}},
	{"gemma template", "<|turn>user\nWhat is 2+2?<turn|>\n<|turn>model\n", []int{
		105, 2364, 107, 3689, 563, 236743, 236778, 236862, 236778, 236881, 106, 107, 105, 4368, 107,
	}},
	{"newlines", "line1\nline2\n\nline3  double", []int{
		1257, 236770, 107, 1257, 236778, 108, 1257, 236800, 138, 7902,
	}},
	{"tab code", "code:\tfunc main() { fmt.Println(\"hi\") }", []int{
		3970, 236787, 255968, 6823, 1689, 825, 642, 22766, 236761, 29006, 885, 2202, 1373, 682,
	}},
	{"gemma native tool call", "<|tool_call>call:read_file{path:<|\"|>x.go<|\"|>}<tool_call|>", []int{
		48, 6639, 236787, 1399, 236779, 2164, 236782, 2337, 236787, 52, 236781, 236761, 1909, 52, 236783, 49,
	}},
}

func TestGemmaTokenizerParity(t *testing.T) {
	dir := os.Getenv("HOME") + "/dev/llm-models/gemma-4-e2b-it-4bit"
	if _, err := os.Stat(filepath.Join(dir, "tokenizer.json")); err != nil {
		t.Skip("model not found")
	}
	tok, err := llm.LoadTokenizer(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		t.Fatalf("LoadTokenizer: %v", err)
	}

	for _, tc := range parityCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tok.Encode(tc.text)
			if tc.want != nil {
				t.Logf("text=%q\ngot  =%v\nwant =%v", tc.text, got, tc.want)
				if len(got) != len(tc.want) {
					t.Fatalf("token count mismatch: got %d, want %d\ngot:  %v\nwant: %v", len(got), len(tc.want), got, tc.want)
				}
				for i := range got {
					if got[i] != tc.want[i] {
						t.Fatalf("token %d mismatch: got %d, want %d\ngot:  %v\nwant: %v", i, got[i], tc.want[i], got, tc.want)
					}
				}
			}
			// Round-trip: decode(encode(text)) should reproduce text.
			decoded := tok.Decode(got)
			if decoded != tc.text {
				t.Errorf("round-trip mismatch:\n in : %q\n out: %q", tc.text, decoded)
			}
		})
	}
}

// TestGemmaTokenizerDecodeUTF8 checks Decode on non-ASCII vocab tokens.
// Gemma's vocab carries raw Unicode tokens (ā, 你, 😀); the decode path must
// not run them through the GPT-2 byte-level decoder.
func TestGemmaTokenizerDecodeUTF8(t *testing.T) {
	dir := os.Getenv("HOME") + "/dev/llm-models/gemma-4-e2b-it-4bit"
	if _, err := os.Stat(filepath.Join(dir, "tokenizer.json")); err != nil {
		t.Skip("model not found")
	}
	tok, err := llm.LoadTokenizer(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		t.Fatalf("LoadTokenizer: %v", err)
	}

	cases := map[string][]int{
		// HF: t.encode(s).ids for each string.
		"Rīga":          nil, // filled below via Encode
		"naïve café":    nil,
		"你好世界":          nil,
		"héllo wörld":   nil,
		"emoji 🤖 test":  nil,
		"žluťoučký kůň": nil,
	}
	for text := range cases {
		ids := tok.Encode(text)
		decoded := tok.Decode(ids)
		t.Logf("%q -> ids=%v -> %q", text, ids, decoded)
		if decoded != text {
			t.Errorf("round-trip mismatch for %q: got %q", text, decoded)
		}
	}
}
