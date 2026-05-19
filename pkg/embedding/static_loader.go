package embedding

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	magicNumber  = "SEMB"
	modelVersion = 3 // v3 = float32 + unigram Viterbi; v2 = float32; v1 = int8 (legacy)
)

// StaticModel holds all the data from a static model binary.
// Supports int8 (v1), float32 (v2), and Unigram Viterbi (v3) embedding formats.
type StaticModel struct {
	dims            int
	vocabSize       int
	padID           uint16
	unkID           uint16
	usesSpacePrefix bool
	tokenizerType   uint8 // 0=BPE, 1=WordPiece, 2=Unigram

	// Embedding vocab (32k quantized tokens): token string -> embedding ID
	vocabMap map[string]uint16

	// Embedding table: [vocabSize][dims] as float32 (v2/v3) or int8 (v1, converted on read)
	embeddingsF32 []float32 // used for v2/v3 (float32)
	embeddingsI8  []int8    // used for v1 (int8, legacy)
	isFloat32     bool      // true if embeddings are stored as float32

	// Merges: pairs of token IDs for BPE (not used in greedy tokenizer but kept for reference)
	merges [][2]uint16

	// --- v3-only fields for Unigram Viterbi ---

	// Full unigram vocab (276k tokens): ordered by tokenizer ID
	vocabFull []string

	// vocabFullMap: token string -> tokenizer ID
	vocabFullMap map[string]uint32

	// Weights: log probability for each tokenizer ID (same length as vocabFull)
	weights []float32

	// Mapping: tokenizer ID -> embedding ID
	// For v3 models, this maps the full tokenizer vocab into the quantized embedding vocab.
	// mapping[tokenizerID] = embeddingID (or -1 if unmapped)
	mapping []int32
}

// LoadStaticModel parses a static model binary file and returns a StaticModel.
// Supports v1 (int8 embeddings), v2 (float32 embeddings), and v3 (Unigram Viterbi).
func LoadStaticModel(data []byte) (*StaticModel, error) {
	if len(data) < 18 {
		return nil, fmt.Errorf("file too small to be a valid model (got %d bytes)", len(data))
	}

	// Read header
	magic := string(data[0:4])
	if magic != magicNumber {
		return nil, fmt.Errorf("invalid magic number: %q (expected %q)", magic, magicNumber)
	}

	version := data[4]
	if version > modelVersion {
		return nil, fmt.Errorf("unsupported version: %d (max supported %d)", version, modelVersion)
	}

	dims := int(binary.LittleEndian.Uint16(data[5:7]))
	vocabSize := int(binary.LittleEndian.Uint16(data[7:9]))
	mergeCount := int(binary.LittleEndian.Uint32(data[9:13]))
	padID := binary.LittleEndian.Uint16(data[13:15])
	unkID := binary.LittleEndian.Uint16(data[15:17])

	// TokenizerType: uint8
	// 0 = BPE/SentencePiece (▁ space prefix), 1 = WordPiece (## subword prefix), 2 = Unigram (▁ space prefix)
	tokenizerType := data[17]
	usesSpacePrefix := tokenizerType == 0 || tokenizerType == 2

	model := &StaticModel{
		dims:            dims,
		vocabSize:       vocabSize,
		padID:           padID,
		unkID:           unkID,
		usesSpacePrefix: usesSpacePrefix,
		tokenizerType:   tokenizerType,
		vocabMap:        make(map[string]uint16, vocabSize),
		merges:          make([][2]uint16, mergeCount),
	}

	switch version {
	case 3:
		return loadV3(model, data, dims, vocabSize, mergeCount)
	case 2:
		return loadV2(model, data, dims, vocabSize, mergeCount)
	default:
		return loadV1(model, data, dims, vocabSize, mergeCount)
	}
}

// parseVocabBlock reads the embedding vocab strings (vocabSize tokens) starting at offset.
// Returns the next offset after the vocab block.
func parseVocabBlock(data []byte, model *StaticModel, vocabSize int, offset int) (int, error) {
	for i := 0; i < vocabSize; i++ {
		if offset+2 > len(data) {
			return 0, fmt.Errorf("unexpected EOF reading vocab length at token %d", i)
		}
		tokenLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2

		if offset+tokenLen > len(data) {
			return 0, fmt.Errorf("unexpected EOF reading token data at token %d", i)
		}
		token := string(data[offset : offset+tokenLen])
		offset += tokenLen

		model.vocabMap[token] = uint16(i)
	}
	return offset, nil
}

// parseMergesBlock reads the merge rules (mergeCount pairs of uint16) starting at offset.
func parseMergesBlock(data []byte, model *StaticModel, mergeCount int, offset int) (int, error) {
	mergesEnd := offset + mergeCount*4
	if mergesEnd > len(data) {
		return 0, fmt.Errorf("unexpected EOF reading merges (need %d bytes, have %d)",
			mergesEnd-offset, len(data)-offset)
	}
	for i := 0; i < mergeCount; i++ {
		id1 := binary.LittleEndian.Uint16(data[offset : offset+2])
		id2 := binary.LittleEndian.Uint16(data[offset+2 : offset+4])
		model.merges[i] = [2]uint16{id1, id2}
		offset += 4
	}
	return offset, nil
}

// parseFullVocabBlock reads the full unigram vocab strings (vocabSizeFull tokens) starting at offset.
// Each token is prefixed with a 4-byte length (uint32) to handle large vocab sizes.
func parseFullVocabBlock(data []byte, vocabSizeFull int, offset int) ([]string, map[string]uint32, int, error) {
	vocabFull := make([]string, vocabSizeFull)
	vocabFullMap := make(map[string]uint32, vocabSizeFull)

	for i := 0; i < vocabSizeFull; i++ {
		if offset+4 > len(data) {
			return nil, nil, 0, fmt.Errorf("unexpected EOF reading full vocab length at token %d", i)
		}
		tokenLen := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4

		if offset+tokenLen > len(data) {
			return nil, nil, 0, fmt.Errorf("unexpected EOF reading full vocab token data at token %d", i)
		}
		token := string(data[offset : offset+tokenLen])
		offset += tokenLen

		vocabFull[i] = token
		vocabFullMap[token] = uint32(i)
	}
	return vocabFull, vocabFullMap, offset, nil
}

// parseFloat32Array reads a []float32 from data starting at offset, reading count elements.
func parseFloat32Array(data []byte, count int, offset int) ([]float32, int, error) {
	expectedEnd := offset + count*4
	if expectedEnd > len(data) {
		return nil, 0, fmt.Errorf("unexpected EOF reading float32 array (need %d bytes, have %d)",
			count*4, len(data)-offset)
	}
	result := make([]float32, count)
	for i := 0; i < count; i++ {
		b := data[offset+i*4 : offset+i*4+4]
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(b))
	}
	return result, offset + count*4, nil
}

// parseInt32Array reads a []int32 from data starting at offset, reading count elements.
func parseInt32Array(data []byte, count int, offset int) ([]int32, int, error) {
	expectedEnd := offset + count*4
	if expectedEnd > len(data) {
		return nil, 0, fmt.Errorf("unexpected EOF reading int32 array (need %d bytes, have %d)",
			count*4, len(data)-offset)
	}
	result := make([]int32, count)
	for i := 0; i < count; i++ {
		result[i] = int32(binary.LittleEndian.Uint32(data[offset+i*4 : offset+i*4+4]))
	}
	return result, offset + count*4, nil
}

// loadV3 handles v3 format: embedding vocab + merges + embeddings + full vocab + weights + mapping.
//
// Binary layout (v3):
//
//	Header (19 bytes): magic(4) + version(1) + dims(2) + vocabSize(2) + mergeCount(4) +
//	                   padID(2) + unkID(2) + tokenizerType(1)
//	Embedding vocab: vocabSize tokens (each: uint32 length + UTF-8 bytes)
//	Merges: mergeCount × 2 × uint16
//	Embeddings: vocabSize × dims float32
//	VocabSizeFull: uint32
//	Full vocab: vocabSizeFull tokens (each: uint32 length + UTF-8 bytes)
//	Weights: vocabSizeFull float32 (log probabilities)
//	Mapping: vocabSizeFull int32 (tokenizer ID -> embedding ID)
func loadV3(model *StaticModel, data []byte, dims, vocabSize, mergeCount int) (*StaticModel, error) {
	offset := 18

	// Parse embedding vocab block (uint32 length prefix for v3)
	for i := 0; i < vocabSize; i++ {
		if offset+4 > len(data) {
			return nil, fmt.Errorf("unexpected EOF reading vocab length at token %d", i)
		}
		tokenLen := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		offset += 4
		if offset+tokenLen > len(data) {
			return nil, fmt.Errorf("unexpected EOF reading vocab token at %d", i)
		}
		token := string(data[offset : offset+tokenLen])
		offset += tokenLen
		model.vocabMap[token] = uint16(i)
	}

	// Parse merges
	offset, err := parseMergesBlock(data, model, mergeCount, offset)
	if err != nil {
		return nil, err
	}

	// Parse embeddings (vocabSize × dims float32)
	model.isFloat32 = true
	elemCount := vocabSize * dims
	model.embeddingsF32, offset, err = parseFloat32Array(data, elemCount, offset)
	if err != nil {
		return nil, err
	}

	// Read vocabSizeFull
	if offset+4 > len(data) {
		return nil, fmt.Errorf("unexpected EOF reading vocabSizeFull")
	}
	vocabSizeFull := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
	offset += 4

	// Parse full unigram vocab
	model.vocabFull, model.vocabFullMap, offset, err = parseFullVocabBlock(data, vocabSizeFull, offset)
	if err != nil {
		return nil, err
	}

	// Parse weights (vocabSizeFull float32)
	model.weights, offset, err = parseFloat32Array(data, vocabSizeFull, offset)
	if err != nil {
		return nil, err
	}

	// Parse mapping (vocabSizeFull int32)
	model.mapping, offset, err = parseInt32Array(data, vocabSizeFull, offset)
	if err != nil {
		return nil, err
	}

	return model, nil
}

// loadV2 handles v2 format: embedding vocab + merges + float32 embeddings.
func loadV2(model *StaticModel, data []byte, dims, vocabSize, mergeCount int) (*StaticModel, error) {
	offset := 18

	// Parse embedding vocab
	offset, err := parseVocabBlock(data, model, vocabSize, offset)
	if err != nil {
		return nil, err
	}

	// Parse merges
	offset, err = parseMergesBlock(data, model, mergeCount, offset)
	if err != nil {
		return nil, err
	}

	// Parse float32 embeddings
	model.isFloat32 = true
	elemCount := vocabSize * dims
	model.embeddingsF32, offset, _ = parseFloat32Array(data, elemCount, offset)
	if err != nil {
		return nil, err
	}

	return model, nil
}

// loadV1 handles v1 format: embedding vocab + merges + int8 embeddings.
func loadV1(model *StaticModel, data []byte, dims, vocabSize, mergeCount int) (*StaticModel, error) {
	offset := 18

	// Parse embedding vocab
	offset, err := parseVocabBlock(data, model, vocabSize, offset)
	if err != nil {
		return nil, err
	}

	// Parse merges
	offset, err = parseMergesBlock(data, model, mergeCount, offset)
	if err != nil {
		return nil, err
	}

	// Parse int8 embeddings
	elemCount := vocabSize * dims
	dataEnd := offset + elemCount
	if dataEnd > len(data) {
		return nil, fmt.Errorf("unexpected EOF reading int8 embeddings (need %d bytes, have %d)",
			elemCount, len(data)-offset)
	}
	model.isFloat32 = false
	model.embeddingsI8 = make([]int8, elemCount)
	for i := 0; i < elemCount; i++ {
		model.embeddingsI8[i] = int8(data[offset+i])
	}

	return model, nil
}

// Validate performs basic validation checks on the model.
func (m *StaticModel) Validate() error {
	if m.dims <= 0 || m.dims > 1024 {
		return errors.New("invalid dimensions")
	}
	if m.vocabSize <= 0 || m.vocabSize > 100000 {
		return errors.New("invalid vocab size")
	}
	if len(m.vocabMap) != m.vocabSize {
		return fmt.Errorf("vocab map size %d doesn't match vocabSize %d",
			len(m.vocabMap), m.vocabSize)
	}
	expectedElemCount := m.vocabSize * m.dims
	if m.isFloat32 {
		if len(m.embeddingsF32) != expectedElemCount {
			return fmt.Errorf("float32 embeddings size %d doesn't match vocabSize*dims %d",
				len(m.embeddingsF32), expectedElemCount)
		}
	} else {
		if len(m.embeddingsI8) != expectedElemCount {
			return fmt.Errorf("int8 embeddings size %d doesn't match vocabSize*dims %d",
				len(m.embeddingsI8), expectedElemCount)
		}
	}

	// Validate v3-specific fields
	if m.tokenizerType == 2 { // Unigram
		if m.vocabFull == nil || m.vocabFullMap == nil || m.weights == nil || m.mapping == nil {
			if m.isFloat32 && m.vocabSize > 10000 {
				// This is a v2 Unigram model (greedy). Allow it for backward compatibility.
			} else if m.vocabFull != nil {
				// v3 model — validate consistency
				if len(m.vocabFull) != len(m.weights) {
					return fmt.Errorf("vocabFull size %d doesn't match weights size %d",
						len(m.vocabFull), len(m.weights))
				}
				if len(m.vocabFull) != len(m.mapping) {
					return fmt.Errorf("vocabFull size %d doesn't match mapping size %d",
						len(m.vocabFull), len(m.mapping))
				}
			}
		} else {
			// v3 model with full vocab — validate consistency
			if len(m.vocabFull) != len(m.weights) {
				return fmt.Errorf("vocabFull size %d doesn't match weights size %d",
					len(m.vocabFull), len(m.weights))
			}
			if len(m.vocabFull) != len(m.mapping) {
				return fmt.Errorf("vocabFull size %d doesn't match mapping size %d",
					len(m.vocabFull), len(m.mapping))
			}
		}
	}

	return nil
}

// Dims returns the embedding dimensionality.
func (m *StaticModel) Dims() int { return m.dims }

// VocabSize returns the vocabulary size.
func (m *StaticModel) VocabSize() int { return m.vocabSize }

// UnkID returns the UNK token ID.
func (m *StaticModel) UnkID() uint16 { return m.unkID }

// PadID returns the PAD token ID.
func (m *StaticModel) PadID() uint16 { return m.padID }

// UsesSpacePrefix returns true if the model uses BPE/SentencePiece space prefix.
func (m *StaticModel) UsesSpacePrefix() bool { return m.usesSpacePrefix }

// IsFloat32 returns true if the model uses float32 embeddings (v2 format).
func (m *StaticModel) IsFloat32() bool { return m.isFloat32 }

// IsUnigram returns true if the model uses Unigram tokenization.
func (m *StaticModel) IsUnigram() bool { return m.tokenizerType == 2 }

// HasViterbiData returns true if this model has full unigram vocab + weights + mapping
// for proper Viterbi decoding (v3 format).
func (m *StaticModel) HasViterbiData() bool {
	return m.vocabFull != nil && m.vocabFullMap != nil && m.weights != nil && m.mapping != nil
}

// GetEmbedding returns the raw embedding vector for a given token ID.
// For v2 (float32) models returns float32; for v1 (int8) returns int8.
func (m *StaticModel) GetEmbedding(id int) []int8 {
	if id < 0 || id >= m.vocabSize {
		return nil
	}
	if m.isFloat32 {
		// Convert float32 slice to int8 for backward compatibility
		offset := id * m.dims
		result := make([]int8, m.dims)
		for i := 0; i < m.dims; i++ {
			result[i] = int8(m.embeddingsF32[offset+i])
		}
		return result
	}
	offset := id * m.dims
	return m.embeddingsI8[offset : offset+m.dims]
}

// GetEmbeddingF32 returns the float32 embedding vector for a given token ID.
// Only valid for v2 (float32) models; returns nil for v1.
func (m *StaticModel) GetEmbeddingF32(id int) []float32 {
	if id < 0 || id >= m.vocabSize {
		return nil
	}
	if !m.isFloat32 {
		return nil
	}
	offset := id * m.dims
	return m.embeddingsF32[offset : offset+m.dims]
}
