package embedding

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	magicNumber  = "SEMB"
	modelVersion = 1
)

// StaticModel holds all the data from the binary model file.
type StaticModel struct {
	dims            int
	vocabSize       int
	padID           uint16
	unkID           uint16
	usesSpacePrefix bool

	// Token lookup: token string -> ID
	vocabMap map[string]uint16

	// Embedding table: [vocabSize][dims] int8 (flat array)
	embeddings []int8

	// Merges: pairs of token IDs for BPE (not used in greedy tokenizer but kept for reference)
	merges [][2]uint16
}

// LoadStaticModel parses a binary model file and returns a StaticModel.
func LoadStaticModel(data []byte) (*StaticModel, error) {
	if len(data) < 18 {
		return nil, fmt.Errorf("file too small to be a valid model (got %d bytes)", len(data))
	}

	// Read header
	// Magic: 4 bytes
	magic := string(data[0:4])
	if magic != magicNumber {
		return nil, fmt.Errorf("invalid magic number: %q (expected %q)", magic, magicNumber)
	}

	// Version: uint8
	version := data[4]
	if version != modelVersion {
		return nil, fmt.Errorf("unsupported version: %d (expected %d)", version, modelVersion)
	}

	// Dims: uint16
	dims := int(binary.LittleEndian.Uint16(data[5:7]))

	// VocabSize: uint16
	vocabSize := int(binary.LittleEndian.Uint16(data[7:9]))

	// MergeCount: uint32
	mergeCount := int(binary.LittleEndian.Uint32(data[9:13]))

	// PadTokenID: uint16
	padID := binary.LittleEndian.Uint16(data[13:15])

	// UnkTokenID: uint16
	unkID := binary.LittleEndian.Uint16(data[15:17])

	// SpacePrefix: uint8
	spacePrefix := data[17]
	usesSpacePrefix := spacePrefix == 1

	model := &StaticModel{
		dims:            dims,
		vocabSize:       vocabSize,
		padID:           padID,
		unkID:           unkID,
		usesSpacePrefix: usesSpacePrefix,
		vocabMap:        make(map[string]uint16, vocabSize),
		merges:          make([][2]uint16, mergeCount),
	}

	// Parse vocabulary block
	offset := 18
	for i := 0; i < vocabSize; i++ {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("unexpected EOF reading vocab length at token %d", i)
		}
		tokenLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2

		if offset+tokenLen > len(data) {
			return nil, fmt.Errorf("unexpected EOF reading token data at token %d", i)
		}
		token := string(data[offset : offset+tokenLen])
		offset += tokenLen

		model.vocabMap[token] = uint16(i)
	}

	// Parse merges block (2x uint16 per merge)
	mergesEnd := offset + mergeCount*4
	if mergesEnd > len(data) {
		return nil, fmt.Errorf("unexpected EOF reading merges (need %d bytes, have %d)",
			mergesEnd-offset, len(data)-offset)
	}
	for i := 0; i < mergeCount; i++ {
		id1 := binary.LittleEndian.Uint16(data[offset : offset+2])
		id2 := binary.LittleEndian.Uint16(data[offset+2 : offset+4])
		model.merges[i] = [2]uint16{id1, id2}
		offset += 4
	}

	// Parse embeddings block (int8[vocabSize][dims])
	embeddingSize := vocabSize * dims
	embeddingsEnd := offset + embeddingSize
	if embeddingsEnd > len(data) {
		return nil, fmt.Errorf("unexpected EOF reading embeddings (need %d bytes, have %d)",
			embeddingSize, len(data)-offset)
	}
	model.embeddings = make([]int8, embeddingSize)
	for i := 0; i < embeddingSize; i++ {
		model.embeddings[i] = int8(data[offset+i])
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
	expectedEmbeddingSize := m.vocabSize * m.dims
	if len(m.embeddings) != expectedEmbeddingSize {
		return fmt.Errorf("embeddings size %d doesn't match vocabSize*dims %d",
			len(m.embeddings), expectedEmbeddingSize)
	}
	return nil
}
