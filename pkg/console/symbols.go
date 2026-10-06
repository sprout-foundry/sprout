package console

import (
	"os"
	"runtime"
	"strings"
	"sync/atomic"
)

// Symbols is the set of non-ASCII glyphs the CLI draws. Every glyph
// written to the terminal should come from Sym() so a non-UTF-8 locale
// (or SPROUT_ASCII=1) gets a readable ASCII rendering instead of mojibake.
type Symbols struct {
	Success  string
	Error    string
	Warning  string
	Info     string
	Action   string
	Paused   string
	Stopped  string
	Middot   string
	Arrow    string
	Prompt   string
	Ellipsis string
	Bullet   string
	HRule    string
	VRule    string
	RuleThin string
	Bar      string
	SubItem  string
	Fold     string
	Caret    string
	Spinner  []string

	BoxTopLeft     string
	BoxTopRight    string
	BoxBottomLeft  string
	BoxBottomRight string
	BoxTeeRight    string
}

var unicodeSymbols = Symbols{
	Success:  "✓",
	Error:    "✗",
	Warning:  "⚠",
	Info:     "ⓘ",
	Action:   "→",
	Paused:   "⏸",
	Stopped:  "⏹",
	Middot:   "·",
	Arrow:    "→",
	Prompt:   "▸",
	Ellipsis: "…",
	Bullet:   "•",
	HRule:    "─",
	VRule:    "│",
	RuleThin: "⎯",
	Bar:      "▌",
	SubItem:  "↳",
	Fold:     "▽",
	Caret:    "▏",
	Spinner:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},

	BoxTopLeft:     "╭",
	BoxTopRight:    "╮",
	BoxBottomLeft:  "╰",
	BoxBottomRight: "╯",
	BoxTeeRight:    "├",
}

var asciiSymbols = Symbols{
	Success:  "+",
	Error:    "x",
	Warning:  "!",
	Info:     "i",
	Action:   ">",
	Paused:   "||",
	Stopped:  "[]",
	Middot:   "-",
	Arrow:    "->",
	Prompt:   ">",
	Ellipsis: "...",
	Bullet:   "*",
	HRule:    "-",
	VRule:    "|",
	RuleThin: "-",
	Bar:      "|",
	SubItem:  "`-",
	Fold:     "v",
	Caret:    "_",
	Spinner:  []string{"|", "/", "-", "\\"},

	BoxTopLeft:     "+",
	BoxTopRight:    "+",
	BoxBottomLeft:  "+",
	BoxBottomRight: "+",
	BoxTeeRight:    "+",
}

var activeSymbols atomic.Pointer[Symbols]

// Sym returns the glyph set for this process: ASCII when SPROUT_ASCII is
// set or the locale is not UTF-8, Unicode otherwise. Resolved once.
func Sym() *Symbols {
	if s := activeSymbols.Load(); s != nil {
		return s
	}
	s := &unicodeSymbols
	if !unicodeOutputSupported() {
		s = &asciiSymbols
	}
	activeSymbols.CompareAndSwap(nil, s)
	return activeSymbols.Load()
}

// SetASCIISymbols forces the ASCII (true) or Unicode (false) glyph set and
// returns whether ASCII was active before, so tests can restore it.
func SetASCIISymbols(ascii bool) bool {
	prev := Sym() == &asciiSymbols
	if ascii {
		activeSymbols.Store(&asciiSymbols)
	} else {
		activeSymbols.Store(&unicodeSymbols)
	}
	return prev
}

// ASCIISymbols reports whether the ASCII glyph set is active.
func ASCIISymbols() bool {
	return Sym() == &asciiSymbols
}

func unicodeOutputSupported() bool {
	switch strings.ToLower(os.Getenv("SPROUT_ASCII")) {
	case "1", "true", "yes", "on":
		return false
	}
	if runtime.GOOS == "windows" {
		return !vtUnsupported.Load()
	}
	return localeIsUTF8()
}

// localeIsUTF8 follows the POSIX precedence LC_ALL > LC_CTYPE > LANG. An
// entirely unset locale is treated as UTF-8: minimal containers and SSH
// sessions commonly omit it while their terminals still render UTF-8, and
// an explicit non-UTF-8 setting is the reliable signal to fall back.
func localeIsUTF8() bool {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		v := os.Getenv(name)
		if v == "" {
			continue
		}
		lower := strings.ToLower(v)
		return strings.Contains(lower, "utf-8") || strings.Contains(lower, "utf8")
	}
	return true
}
