package embedding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGemmaTokenizer_BasicEncode(t *testing.T) {
	tmpDir := t.TempDir()
	// Minimal BPE tokenizer with a small vocab (no pre_tokenizer — uses fallback split)
	jsonContent := `{
		"model": {
			"type": "BPE",
			"vocab": {
				"<pad>": 0,
				"<s>": 1,
				"</s>": 2,
				"<unk>": 3,
				"<bos>": 4,
				"<eos>": 5,
				"a": 6,
				"b": 7,
				"c": 8,
				"ab": 9,
				"bc": 10,
				"abc": 11,
				" hello": 12,
				"world": 13
			},
			"merges": ["a b ab", "b c bc", "ab c abc"]
		}
	}`
	path := filepath.Join(tmpDir, "tokenizer.json")
	if err := os.WriteFile(path, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}
	tok, err := NewGemmaTokenizer(path)
	if err != nil {
		t.Fatalf("NewGemmaTokenizer: %v", err)
	}
	// Test Encode
	ids := tok.Encode("abc")
	if len(ids) == 0 {
		t.Fatal("expected non-zero token IDs")
	}
	// Test Decode produces something non-empty
	text := tok.Decode(ids)
	if text == "" {
		t.Fatal("Decode returned empty string")
	}
	// Test empty input
	empty := tok.Encode("")
	if len(empty) != 0 {
		t.Fatalf("expected empty tokens for empty input, got %d", len(empty))
	}
	// Test EncodeWithBOSAndEOS
	withMarkers := tok.EncodeWithBOSAndEOS("abc")
	if len(withMarkers) <= len(ids) {
		t.Fatal("expected BOS/EOS tokens to increase length")
	}
}

func TestGemmaTokenizer_ByteEncoding(t *testing.T) {
	// Test byte encoding for non-ASCII characters
	tmpDir := t.TempDir()
	jsonContent := `{
		"model": {
			"type": "BPE",
			"vocab": {"<unk>": 0},
			"merges": []
		}
	}`
	path := filepath.Join(tmpDir, "tokenizer.json")
	if err := os.WriteFile(path, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}
	tok, err := NewGemmaTokenizer(path)
	if err != nil {
		t.Fatalf("NewGemmaTokenizer: %v", err)
	}
	// Non-ASCII text should be byte-encoded
	encoded := tok.byteEncode("hello")
	if encoded != "hello" {
		t.Errorf("ASCII bytes should be preserved: got %q", encoded)
	}
}

func TestGemmaTokenizer_VocabSize(t *testing.T) {
	tmpDir := t.TempDir()
	jsonContent := `{
		"model": {
			"type": "BPE",
			"vocab": {"a": 0, "b": 1, "c": 2},
			"merges": ["a b ab"]
		}
	}`
	path := filepath.Join(tmpDir, "tokenizer.json")
	if err := os.WriteFile(path, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}
	tok, err := NewGemmaTokenizer(path)
	if err != nil {
		t.Fatal(err)
	}
	if tok.VocabSize() != 3 {
		t.Errorf("expected vocab size 3, got %d", tok.VocabSize())
	}
}
