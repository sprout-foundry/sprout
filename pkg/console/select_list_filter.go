// Select list filter logic
package console

import (
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// filterAppend adds runes to the filter and refilters.
func (s *SelectList) filterAppend(text string) {
	s.mu.Lock()
	s.filter += text
	s.mu.Unlock()
	s.applyFilter(s.filter)
}

// filterBackspace removes the last rune from the filter.
func (s *SelectList) filterBackspace() {
	s.mu.Lock()
	if s.filter == "" {
		s.mu.Unlock()
		return
	}
	_, size := utf8.DecodeLastRuneInString(s.filter)
	s.filter = s.filter[:len(s.filter)-size]
	s.mu.Unlock()
	s.applyFilter(s.filter)
}

// consumeUTF8 collects the continuation bytes that follow a UTF-8
// lead byte and appends the resulting rune to the filter. n is the
// number of bytes already in buf[]; lead is at buf[0].
func (s *SelectList) consumeUTF8(lead byte, n int, buf []byte) {
	expected := utf8Width(lead)
	collected := buf[:n]
	deadline := time.Now().Add(30 * time.Millisecond)
	for len(collected) < expected && time.Now().Before(deadline) {
		var more [4]byte
		m, _ := os.Stdin.Read(more[:expected-len(collected)])
		if m > 0 {
			collected = append(collected, more[:m]...)
		} else {
			time.Sleep(1 * time.Millisecond)
		}
	}
	if r, _ := utf8.DecodeRune(collected); r != utf8.RuneError {
		s.filterAppend(string(r))
	}
}

// utf8Width returns the expected total byte count for a UTF-8 sequence
// given its lead byte. 1 for single-byte (shouldn't happen for our
// callers), 2/3/4 for multi-byte.
func utf8Width(b byte) int {
	switch {
	case b&0xE0 == 0xC0:
		return 2
	case b&0xF0 == 0xE0:
		return 3
	case b&0xF8 == 0xF0:
		return 4
	default:
		return 1
	}
}

// utf8RuneFromBuf decodes the bytes in buf (starting at buf[0]=lead)
// into a string. n is the number of bytes read so far; continuation
// bytes already present in buf are used directly. If the buffer holds
// an incomplete sequence, the decoded bytes are still returned (a
// RuneError surfaces as "\ufffd", which callers may forward as-is).
func utf8RuneFromBuf(lead byte, n int, buf []byte) string {
	if n >= utf8Width(lead) {
		// We already have the full sequence in buf.
		if r, size := utf8.DecodeRune(buf[:n]); size > 0 {
			return string(r)
		}
	}
	// Incomplete — best effort: decode what we have.
	if r, _ := utf8.DecodeRune(buf[:n]); r != utf8.RuneError {
		return string(r)
	}
	return ""
}

// applyFilter recomputes the filtered slice from opts.Items using the
// current filter. Resets cursor/offset to 0 because positions don't
// translate across filter changes.
func (s *SelectList) applyFilter(filter string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filter = filter
	s.filtered = s.filtered[:0]
	if filter == "" {
		for i := range s.opts.Items {
			s.filtered = append(s.filtered, i)
		}
	} else {
		needle := strings.ToLower(filter)
		for i, item := range s.opts.Items {
			hay := strings.ToLower(item.Label + " " + item.Detail)
			if strings.Contains(hay, needle) {
				s.filtered = append(s.filtered, i)
			}
		}
	}
	s.cursor = 0
	s.offset = 0
}
