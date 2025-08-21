package filesystem

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/alantheprice/ledit/pkg/ui"
)

// SaveFile saves or removes a file with the given content.
// If content is empty, the file is removed.
func SaveFile(filename, content string) error {
	if content == "" {
		if _, err := os.Stat(filename); err == nil {
			// File exists, remove it
			ui.Out().Printf("🗑️  Removing file: %s\n", filename)
			return os.Remove(filename)
		} else if os.IsNotExist(err) {
			// File does not exist, nothing to do
			return nil
		} else {
			// Other error checking file stat
			return fmt.Errorf("error checking file %s: %w", filename, err)
		}
	}

	// Notify user about file being written
	ui.Out().Printf("💾 Writing file: %s (%d bytes)\n", filename, len(content))

	// Ensure the directory exists
	dir := filepath.Dir(filename)
	if dir != "" {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("could not create directory %s: %w", dir, err)
		}
	}

	// Normalize EOLs to existing file style if present and preserve BOM/UTF-16 when detected
	in := []byte(content)
	normalized := in
	if b, err := os.ReadFile(filename); err == nil {
		// EOL style
		if bytes.Contains(b, []byte("\r\n")) {
			normalized = bytes.ReplaceAll(normalized, []byte("\n"), []byte("\r\n"))
		}
		// BOM/UTF-16 detection (simple heuristics)
		if len(b) >= 2 {
			// UTF-16 LE BOM FF FE
			if b[0] == 0xFF && b[1] == 0xFE {
				// encode as UTF-16 LE with BOM
				u16 := utf16.Encode([]rune(string(content)))
				// write BOM
				buf := []byte{0xFF, 0xFE}
				for _, w := range u16 {
					buf = append(buf, byte(w), byte(w>>8))
				}
				normalized = buf
			}
			// UTF-16 BE BOM FE FF
			if b[0] == 0xFE && b[1] == 0xFF {
				u16 := utf16.Encode([]rune(string(content)))
				buf := []byte{0xFE, 0xFF}
				for _, w := range u16 {
					buf = append(buf, byte(w>>8), byte(w))
				}
				normalized = buf
			}
			// UTF-8 BOM EF BB BF
			if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
				// Ensure output starts with UTF-8 BOM
				if !bytes.HasPrefix(normalized, []byte{0xEF, 0xBB, 0xBF}) {
					normalized = append([]byte{0xEF, 0xBB, 0xBF}, normalized...)
				}
			}
		}
	}
	// Ensure UTF-8 validity for normal writes
	if len(normalized) > 0 && !utf8.Valid(normalized) {
		// As a safety, write raw; caller chose encoding (likely UTF-16 block above)
	}
	err := os.WriteFile(filename, normalized, 0644)
	if err != nil {
		ui.Out().Printf("   ❌ Failed to write file: %v\n", err)
	} else {
		ui.Out().Print("   ✅ File written successfully\n")
	}
	return err
}

// ReadFile reads the content of a file.
func ReadFile(filename string) (string, error) {
	ui.Out().Printf("📖 Reading file: %s\n", filename)

	// Use buffered reader for potential large files; still load whole file for simplicity
	f, err := os.Open(filename)
	if err != nil {
		ui.Out().Printf("   ❌ Failed to read file: %v\n", err)
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}
	defer f.Close()
	buf := new(bytes.Buffer)
	if _, err := bufio.NewReader(f).WriteTo(buf); err != nil {
		ui.Out().Printf("   ❌ Failed to read file: %v\n", err)
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}
	content := buf.Bytes()
	if err != nil {
		ui.Out().Printf("   ❌ Failed to read file: %v\n", err)
		return "", fmt.Errorf("could not read file %s: %w", filename, err)
	}

	ui.Out().Printf("   ✅ File read successfully (%d bytes)\n", len(content))
	return string(content), nil
}
