package embedding

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// index_embed.go — the embedding-unit pipeline shared by the build and
// update paths: embedUnits and the embedding-text/record conversion
// helpers. Split out of index.go.

// embedUnits converts CodeUnits to text, batch-embeds, and returns VectorRecords.
// Returns partial results on cancellation. Calls onFileComplete per-file for checkpointed persistence.
func (m *IndexManager) embedUnits(ctx context.Context, units []CodeUnit, repoRoot string, onFileComplete func(file string, records []VectorRecord) error) ([]VectorRecord, error) {
	now := time.Now()
	var records []VectorRecord
	var embedded int
	floorTripped := false // mid-loop floor halt: return errMemFloor with partial records

	// Fail hard below the floor with zero progress: an empty build result
	// would otherwise be indistinguishable from "nothing to index".
	if err := checkMemFloor(); err != nil {
		return nil, err
	}

	// Sort by length to minimize padding waste in batch embedding.
	order := make([]int, len(units))
	textOf := make([]string, len(units))
	for i := range units {
		order[i] = i
		textOf[i] = embeddingText(units[i], m.opts.MaxBodyLen)
	}

	// Embed recently-touched files first so the partial index becomes
	// semantically useful within ~1 min instead of ~17 min on a full build.
	// The store is flushed and queryable during embedding, so ordering
	// determines when useful results appear to the user.
	priority := buildFilePriority(repoRoot, uniqueFiles(units))
	if len(priority) > 0 {
		var t0, t1, t2 int
		for _, u := range units {
			switch priority[u.File] {
			case 0:
				t0++
			case 1:
				t1++
			default:
				t2++
			}
		}
		debugLogf("index: priority tiers — recent: %d, 30d: %d, older: %d units", t0, t1, t2)
	}

	sort.SliceStable(order, func(a, b int) bool {
		pa := priority[units[order[a]].File]
		pb := priority[units[order[b]].File]
		if pa != pb {
			return pa < pb
		}
		return len(textOf[order[a]]) < len(textOf[order[b]])
	})

	// Track vectors by original index for partial results.
	vecByIndex := make([][]float32, len(units))

	// Group unit indices by file so a file's completion can be detected the
	// moment its last unit embeds, no matter which batch that lands in.
	unitsByFile := make(map[string][]int, len(units))
	for i, u := range units {
		unitsByFile[u.File] = append(unitsByFile[u.File], i)
	}
	embeddedByFile := make(map[string]int, len(unitsByFile))
	completedFiles := make(map[string]bool, len(unitsByFile))

	// recordsForFile assembles one file's records from embedded vectors.
	recordsForFile := func(file string) []VectorRecord {
		idxs := unitsByFile[file]
		out := make([]VectorRecord, 0, len(idxs))
		for _, idx := range idxs {
			u := units[idx]
			if u.ID == u.File {
				out = append(out, fileCodeUnitToRecord(u, vecByIndex[idx], now))
			} else {
				out = append(out, codeUnitToRecord(u, vecByIndex[idx], now))
			}
		}
		return out
	}

	// markEmbedded advances per-file progress for one batch and fires
	// onFileComplete for any file whose last unit just landed.
	markEmbedded := func(idxs []int) error {
		if onFileComplete == nil {
			return nil
		}
		for _, idx := range idxs {
			file := units[idx].File
			if completedFiles[file] {
				continue
			}
			embeddedByFile[file]++
			if embeddedByFile[file] < len(unitsByFile[file]) {
				continue
			}
			completedFiles[file] = true
			if err := onFileComplete(file, recordsForFile(file)); err != nil {
				return err
			}
		}
		return nil
	}

	for i := 0; i < len(order); i += m.opts.BatchSize {
		if err := ctx.Err(); err != nil {
			// Return partial results on cancellation; completed files were already flushed.
			log.Printf("index: embedding interrupted after %d/%d units: %v", embedded, len(units), err)
			break
		}
		// Same partial-flush behavior as cancellation: native allocations are
		// invisible to the Go heap limit, so stop a runaway build before the
		// kernel OOM killer picks a victim. Unlike cancellation, this is not a
		// clean stop — callers must be able to see the memory condition.
		if err := checkMemFloor(); err != nil {
			log.Printf("index: embedding halted after %d/%d units: %v", embedded, len(units), err)
			floorTripped = true
			break
		}

		end := i + m.opts.BatchSize
		if end > len(order) {
			end = len(order)
		}

		idxs := order[i:end]
		texts := make([]string, len(idxs))
		for j, idx := range idxs {
			texts[j] = textOf[idx]
		}

		vecs, err := m.provider.EmbedBatchWithPrefix(ctx, texts, documentPrefix)
		if err != nil {
			return records, fmt.Errorf("index: embed batch [%d:%d]: %w", i, end, err)
		}

		for j, idx := range idxs {
			vecByIndex[idx] = vecs[j]
		}
		embedded += len(idxs)

		if err := markEmbedded(idxs); err != nil {
			return records, err
		}

		// Log progress every ProgressInterval records embedded.
		if embedded%ProgressInterval < m.opts.BatchSize {
			debugLogf("index: embedding progress: %d/%d records", embedded, len(units))
			logMemCheckpoint(fmt.Sprintf("embedding-loop (%d/%d)", embedded, len(units)))
		}
	}

	// Emit in input order for per-file grouping.
	for i, u := range units {
		if vecByIndex[i] == nil {
			continue // not reached before cancellation/floor halt
		}
		// File-level units have ID == file path; code units have ID == "file:name".
		if u.ID == u.File {
			// File-level unit
			records = append(records, fileCodeUnitToRecord(u, vecByIndex[i], now))
		} else {
			// Code unit
			records = append(records, codeUnitToRecord(u, vecByIndex[i], now))
		}
	}

	if floorTripped {
		return records, errMemFloor
	}
	return records, nil
}

// embeddingText builds the text to embed from a CodeUnit, with optional body truncation.
func embeddingText(u CodeUnit, maxBodyLen int) string {
	body := u.Body
	if maxBodyLen > 0 && len(body) > maxBodyLen {
		// Truncate to maxBodyLen bytes, snapping back to the last valid
		// UTF-8 character boundary so we don't produce invalid runes.
		// This avoids the cost of converting the entire body to []rune
		// just to truncate it.
		body = truncateUTF8Safe(body, maxBodyLen)
	}
	return u.Signature + "\n" + body
}

// truncateUTF8Safe truncates s to at most maxBytes bytes, snapping back to
// the last valid UTF-8 character boundary if the cut falls mid-character.
func truncateUTF8Safe(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// A UTF-8 continuation byte has the top two bits as 10 (0x80 set, 0x40 clear).
	// If the byte at maxBytes is a continuation byte, we're mid-character — walk
	// backward to find the start of that character.
	for maxBytes > 0 && s[maxBytes]&0xC0 == 0x80 {
		maxBytes--
	}
	return s[:maxBytes]
}

// codeUnitToRecord converts a CodeUnit and its embedding into a VectorRecord.
func codeUnitToRecord(u CodeUnit, embedding []float32, indexedAt time.Time) VectorRecord {
	return VectorRecord{
		ID:        u.ID,
		File:      u.File,
		Name:      u.Name,
		Signature: strings.TrimSpace(u.Signature),
		StartLine: u.StartLine,
		EndLine:   u.EndLine,
		Language:  u.Language,
		Embedding: embedding,
		Hash:      u.Hash,
		IndexedAt: indexedAt,
		Type:      "code_unit", // All code unit records are type "code_unit"
	}
}

// fileCodeUnitToRecord converts a file-level CodeUnit and its embedding into a VectorRecord.
// Sets Type to "file" to distinguish it from code_unit records.
func fileCodeUnitToRecord(u CodeUnit, embedding []float32, indexedAt time.Time) VectorRecord {
	return VectorRecord{
		ID:        u.ID,
		File:      u.File,
		Name:      u.Name,
		Signature: strings.TrimSpace(u.Signature),
		StartLine: u.StartLine,
		EndLine:   u.EndLine,
		Language:  u.Language,
		Embedding: embedding,
		Hash:      u.Hash,
		IndexedAt: indexedAt,
		Type:      "file", // File-level records have type "file"
	}
}

// hasCodeExtension checks if a file path has a code extension (.go, .py, .ts, etc.).
func hasCodeExtension(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".py":
		return true
	default:
		return false
	}
}
