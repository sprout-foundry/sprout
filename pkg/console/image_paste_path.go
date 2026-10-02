package console

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// PastedImageFiles reports whether pasted text is nothing but paths to image
// files — what a terminal pastes when a screenshot or image is dragged in,
// or copied as a file in Finder — and returns them resolved. A paste that
// mixes in anything else stays text, so prose that merely mentions a path is
// not turned into an attachment. Only absolute, ~ and file:// paths count.
func PastedImageFiles(text string) ([]string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	// One raw path first: Finder's "Copy as Pathname" leaves spaces unescaped.
	if p, ok := resolvePastedImagePath(text); ok {
		return []string{p}, true
	}
	tokens := splitPastedPaths(text)
	if len(tokens) == 0 {
		return nil, false
	}
	paths := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		p, ok := resolvePastedImagePath(tok)
		if !ok {
			return nil, false
		}
		paths = append(paths, p)
	}
	return paths, true
}

// AttachPastedImageFile copies an image file into the workspace's pasted
// images directory and returns the in-query placeholder for it. Copying
// matters: screenshot paths live in a temp directory macOS clears within
// minutes, and their names carry a U+202F before AM/PM that trips up shells
// and tools, while the copy gets a stable ASCII name.
func AttachPastedImageFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxPastedImageSize+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxPastedImageSize {
		return "", fmt.Errorf("%s is larger than %d MB", filepath.Base(path), MaxPastedImageSize>>20)
	}
	saved, err := SavePastedImage(data, "")
	if err != nil {
		return "", err
	}
	return PastedImagePlaceholder(saved), nil
}

// splitPastedPaths splits pasted text into shell words: whitespace separates,
// backslash escapes the next character, and quotes group — how terminals
// encode the paths of files dropped onto them.
func splitPastedPaths(text string) []string {
	var tokens []string
	var cur strings.Builder
	inToken := false
	var quote rune
	escaped := false
	for _, r := range text {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inToken = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inToken = r, true
		case unicode.IsSpace(r) && r != '\u202f' && r != '\u00a0':
			if inToken {
				tokens = append(tokens, cur.String())
				cur.Reset()
				inToken = false
			}
		default:
			cur.WriteRune(r)
			inToken = true
		}
	}
	if inToken {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// resolvePastedImagePath turns one pasted path into the path of an existing
// image file, or reports false.
func resolvePastedImagePath(raw string) (string, bool) {
	p := strings.TrimSpace(raw)
	if strings.HasPrefix(p, "file://") {
		u, err := url.Parse(p)
		if err != nil {
			return "", false
		}
		p = u.Path
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		return "", false
	}
	if _, err := os.Stat(p); err != nil {
		var ok bool
		if p, ok = matchNameIgnoringSpaceKind(p); !ok {
			return "", false
		}
	}
	return p, isImageFile(p)
}

// matchNameIgnoringSpaceKind finds the file a pasted path names when the
// paste swapped the special spaces macOS puts in screenshot names (U+202F
// before AM/PM, U+00A0) for plain ones, or the reverse.
func matchNameIgnoringSpaceKind(p string) (string, bool) {
	dir, want := filepath.Dir(p), normalizeSpaces(filepath.Base(p))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if normalizeSpaces(e.Name()) == want {
			return filepath.Join(dir, e.Name()), true
		}
	}
	return "", false
}

func normalizeSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s)
}

func isImageFile(p string) bool {
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 32)
	n, _ := io.ReadFull(f, head)
	ext, _ := DetectImageMagic(head[:n])
	return ext != ""
}

// attachPastedImageFiles attaches each pasted image file and returns the
// placeholders to insert ("" when none could be attached, so the caller
// inserts the paste as text). Progress prints above the prompt.
func attachPastedImageFiles(paths []string) string {
	var placeholders []string
	for _, p := range paths {
		ph, err := AttachPastedImageFile(p)
		if err != nil {
			PrintLine(GlyphError.Prefix() + fmt.Sprintf("Couldn't attach %s: %v", filepath.Base(p), err))
			continue
		}
		PrintLine(GlyphAction.Prefix() + "Attached image " + filepath.Base(p))
		placeholders = append(placeholders, ph)
	}
	if len(placeholders) == 0 {
		return ""
	}
	return strings.Join(placeholders, " ") + " "
}

// attachPastedImageData saves raw image bytes from a paste and returns the
// placeholder to insert, or "" if the bytes are not an image or could not be
// saved.
func attachPastedImageData(data []byte) string {
	if len(data) <= 4 || len(data) > MaxPastedImageSize {
		return ""
	}
	ext, mimeType := DetectImageMagic(data)
	if ext == "" {
		return ""
	}
	saved, err := SavePastedImage(data, "")
	if err != nil {
		PrintLine(GlyphError.Prefix() + "Couldn't save pasted image: " + err.Error())
		return ""
	}
	PrintLine(GlyphAction.Prefix() + fmt.Sprintf("Attached pasted image (%s, %d KB)", mimeType, (len(data)+1023)/1024))
	return PastedImagePlaceholder(saved) + " "
}
