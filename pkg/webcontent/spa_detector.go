package webcontent

import "strings"

// NeedsRendering inspects a raw HTML document and reports whether the page
// appears to be a single-page application (SPA) shell that needs browser
// rendering to produce meaningful content, as opposed to a server-rendered
// page whose text is already extractable from the raw HTML.
//
// The function combines four heuristic signals:
//
//  1. SPA shell pattern — empty mount-point divs such as <div id="root">,
//     <div id="app">, <div id="__next">, etc.
//  2. Framework markers in <script> tags (e.g. __NEXT_DATA__, __NUXT__,
//     script src containing "react", "vue", "angular", "svelte", etc.)
//     — only when combined with low visible text (to avoid SSR false positives).
//  3. Large inline script blocks whose total content exceeds a threshold
//     relative to the overall document length.
//  4. Very low visible-text-to-HTML ratio (the catch-all signal).
//
// It operates on raw strings with simple scanning — no DOM parsing — for
// efficiency on large responses.
func NeedsRendering(html string) bool {
	if len(html) == 0 {
		return false
	}

	// Quick rejection: very short strings are clearly not HTML pages.
	if len(html) < 20 {
		return false
	}

	info := analyzeHTML(html)

	// Signal 0: zero visible text in a document with HTML tags.
	// A page that parses as HTML but renders nothing useful (only tags,
	// no text content) is not going to yield extractable text.
	if info.visibleTextLen == 0 && info.hasHTMLTags {
		return true
	}

	// Signal 1: SPA shell pattern.
	// An empty (or whitespace/noscript-only) mount-point div is a strong
	// indicator regardless of other signals.
	if info.hasEmptyShell {
		return true
	}

	// Signal 2: Low text ratio is a reliable catch-all.
	// Skip on short documents where ratio is unreliable.
	if len(html) >= 200 && info.textRatio() < 0.05 {
		return true
	}

	// Signal 3: Large inline scripts (total script bytes > 50% of HTML).
	if info.largeScripts() {
		// Large scripts alone are not enough if there's plenty of visible text
		// (e.g. a WordPress page with an inline CSS/JS optimizer plugin).
		if !(len(html) >= 200 && info.textRatio() >= 0.08) {
			return true
		}
	}

	// Signal 4: Framework markers + low text content.
	if info.hasFrameworkMarker {
		// Framework markers with lots of visible text → SSR page, not an SPA.
		if info.visibleTextLen < 300 {
			return true
		}
	}

	return false
}

// ---------------------------------------------------------------------------
// Analysis state.
// ---------------------------------------------------------------------------

type htmlAnalysis struct {
	hasEmptyShell      bool
	hasFrameworkMarker bool
	hasHTMLTags        bool // contains at least one real HTML tag
	scriptContentLen   int  // bytes inside <script>…</script> content
	visibleTextLen     int  // non-space chars outside tags/script/style/head
	htmlLen            int  // total len(html)
}

// textRatio returns visibleTextLen / htmlLen.
func (a *htmlAnalysis) textRatio() float64 {
	if a.htmlLen == 0 {
		return 0
	}
	return float64(a.visibleTextLen) / float64(a.htmlLen)
}

// largeScripts returns true when inline script content exceeds 50 % of the
// total HTML byte length.
func (a *htmlAnalysis) largeScripts() bool {
	return a.scriptContentLen > 0 &&
		float64(a.scriptContentLen)/float64(a.htmlLen) > 0.50
}

// ---------------------------------------------------------------------------
// Single-pass HTML analysis.
// ---------------------------------------------------------------------------

// shellPatternIDs are id attribute values that indicate an SPA mount point.
var shellPatternIDs = map[string]bool{
	"root":   true,
	"app":    true,
	"__next": true,
	"__nuxt": true,
	"main":   true,
}

// frameworkMarkerStrings are substrings that, when found inside <script>
// content, indicate a JS framework is present.
var frameworkMarkerStrings = []string{
	"__NEXT_DATA__",
	"__NUXT__",
	"__APP",
}

// frameworkSrcFragments are substrings that indicate a framework library in
// a <script src="…"> attribute.
var frameworkSrcFragments = []string{
	"react", "vue", "angular", "svelte", "next", "nuxt",
}

func analyzeHTML(html string) htmlAnalysis {
	var info htmlAnalysis
	info.htmlLen = len(html)

	lower := strings.ToLower(html)

	info.hasEmptyShell = detectEmptyShell(lower)
	info.hasFrameworkMarker = detectFrameworkMarkers(lower)
	info.hasHTMLTags = detectHTMLTags(html)
	computeContentLengths(html, &info)

	return info
}

// detectHTMLTags returns true if s contains at least one HTML tag
// (a '<' followed eventually by '>').
func detectHTMLTags(s string) bool {
	const maxScan = 200 // only need to scan the beginning
	end := len(s)
	if end > maxScan {
		end = maxScan
	}
	for i := 0; i < end; i++ {
		if s[i] == '<' {
			if j := strings.IndexByte(s[i:], '>'); j >= 0 && j < end-i {
				// Verify it looks like a real tag (< followed by letter or /).
				after := i + 1
				if after < len(s) {
					ch := s[after]
					if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '/' {
						return true
					}
				}
			}
		}
	}
	return false
}

// detectEmptyShell looks for <div id="root"> (and friends) whose immediate
// inner content (up to the matching </div>) is empty or contains only
// whitespace and <noscript>…</noscript> blocks.
func detectEmptyShell(lower string) bool {
	for id := range shellPatternIDs {
		if searchEmptyShell(lower, `<div id="`+id+`"`) {
			return true
		}
	}

	// Check <div class="root"> as well.
	if searchEmptyShell(lower, `<div class="root"`) {
		return true
	}

	// Check <app-root> (Angular).
	if idx := findOutsideComment(lower, `<app-root>`); idx >= 0 {
		if end := strings.Index(lower[idx+10:], `</app-root>`); end >= 0 {
			if isNoscriptOnlyContent(lower[idx+10 : idx+10+end]) {
				return true
			}
		}
	}

	return false
}

// searchEmptyShell searches for occurrences of openTag (e.g. "<div id=\"root\"")
// and checks whether the content up to the matching </div> is empty/whitespace/
// noscript-only.
func searchEmptyShell(lower, openTag string) bool {
	idx := 0
	for {
		pos := findOutsideComment(lower[idx:], openTag)
		if pos < 0 {
			return false
		}
		pos += idx

		// Advance past the opening tag's '>'.
		gt := strings.IndexByte(lower[pos:], '>')
		if gt < 0 {
			return false
		}
		contentStart := pos + gt + 1

		// Find the matching </div> with proper nesting so that
		// <div id="root"><div>content</div></div> — the inner content
		// includes the nested <div> and its text.
		closePos := findMatchingCloseDiv(lower[contentStart:])
		if closePos < 0 {
			return false
		}
		inner := lower[contentStart : contentStart+closePos]

		if isNoscriptOnlyContent(inner) {
			return true
		}

		idx = contentStart
	}
}

// findMatchingCloseDiv returns the index of the matching </div> in s,
// accounting for nested <div>…</div> pairs. Returns -1 if not found.
func findMatchingCloseDiv(s string) int {
	depth := 1
	i := 0
	for depth > 0 && i < len(s) {
		openPos := strings.Index(s[i:], "<div")
		closePos := strings.Index(s[i:], "</div>")
		if closePos < 0 {
			return -1 // no matching close.
		}
		if openPos < 0 || openPos >= closePos {
			// </div> comes first: it closes one level of nesting.
			absClose := i + closePos // absolute position of </div> in s
			depth--
			i = absClose + 6        // skip past </div>
			if depth == 0 {
				return absClose     // return absolute position in s
			}
		} else {
			// <div comes first: open a new nesting level.
			// Verify it's a <div tag, not <divider or <diva.
			afterDiv := openPos + 4
			if afterDiv < len(s) {
				nextCh := s[afterDiv]
				if nextCh == ' ' || nextCh == '>' || nextCh == '/' || nextCh == '\t' || nextCh == '\n' || nextCh == '\r' {
					depth++
					// Skip past the opening <div...> tag.
					tagEnd := strings.IndexByte(s[i+openPos:], '>')
					if tagEnd < 0 {
						return -1
					}
					i = i + openPos + tagEnd + 1
					continue
				}
			}
			// Not a real <divX> — skip past the occurrence.
			i = i + openPos + 4
		}
	}
	return -1
}

// findOutsideComment returns the index of substr in s, skipping any
// occurrence that falls inside an HTML comment (<!-- … -->).
// Returns -1 if not found outside comments.
func findOutsideComment(s, substr string) int {
	idx := 0
	for {
		pos := strings.Index(s[idx:], substr)
		if pos < 0 {
			return -1
		}
		absPos := idx + pos

		// Check if this position is inside an HTML comment.
		if !insideComment(s, absPos) {
			return absPos
		}
		idx = absPos + len(substr)
	}
}

// insideComment reports whether position pos in s falls inside <!-- … -->.
func insideComment(s string, pos int) bool {
	// Find the last <!-- before pos.
	commentStart := -1
	searchFrom := 0
	for {
		cs := strings.Index(s[searchFrom:], "<!--")
		if cs < 0 || searchFrom+cs >= pos {
			break
		}
		commentStart = searchFrom + cs
		searchFrom = commentStart + 4
	}
	if commentStart < 0 {
		return false
	}

	// Find the first --> after commentStart.
	ce := strings.Index(s[commentStart:], "-->")
	if ce < 0 {
		return true // unclosed comment — everything after is "inside"
	}
	commentEnd := commentStart + ce + 3
	return pos < commentEnd
}

// isNoscriptOnlyContent returns true if s is empty, whitespace-only, or
// contains only <noscript>…</noscript> blocks surrounded by whitespace.
func isNoscriptOnlyContent(s string) bool {
	// Strip all <noscript…>…</noscript> blocks.
	var b strings.Builder
	rest := s
	for {
		// Find <noscript> or <noscript ...
		openIdx := strings.Index(rest, "<noscript")
		if openIdx < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:openIdx])

		// Skip past the opening tag.
		tagEnd := strings.IndexByte(rest[openIdx:], '>')
		if tagEnd < 0 {
			break
		}
		afterOpen := openIdx + tagEnd + 1

		// Find </noscript>.
		closeIdx := strings.Index(rest[afterOpen:], "</noscript>")
		if closeIdx < 0 {
			break // unclosed — ignore the rest
		}
		rest = rest[afterOpen+closeIdx+11:]
	}
	return strings.TrimSpace(b.String()) == ""
}

// detectFrameworkMarkers looks for framework identifiers in <script> tag
// content or src attributes.
func detectFrameworkMarkers(lower string) bool {
	const scriptOpen = "<script"
	const scriptClose = "</script>"

	idx := 0
	for {
		pos := strings.Index(lower[idx:], scriptOpen)
		if pos < 0 {
			return false
		}
		pos += idx

		// Ensure we match a complete tag name (<script, not <scripting).
		after := pos + 7 // len("<script")
		if after < len(lower) {
			ch := lower[after]
			if ch != ' ' && ch != '>' && ch != '/' && ch != '\t' && ch != '\n' && ch != '\r' {
				idx = after
				continue
			}
		}

		// Advance to the closing '>' of the opening tag.
		gt := strings.IndexByte(lower[pos:], '>')
		if gt < 0 {
			return false
		}
		tagEnd := pos + gt + 1

		// Check src attribute in the opening tag text.
		openingTag := lower[pos:tagEnd]
		if srcStart := strings.Index(openingTag, `src="`); srcStart >= 0 {
			srcStart += 5
			srcEnd := strings.IndexByte(openingTag[srcStart:], '"')
			if srcEnd >= 0 {
				src := openingTag[srcStart : srcStart+srcEnd]
				for _, frag := range frameworkSrcFragments {
					if strings.Contains(src, frag) {
						return true
					}
				}
			}
		}

		// Find the matching </script>.
		end := strings.Index(lower[tagEnd:], scriptClose)
		if end < 0 {
			return false
		}
		content := lower[tagEnd : tagEnd+end]

		// Check for framework marker strings in content.
		for _, marker := range frameworkMarkerStrings {
			if strings.Contains(content, marker) {
				return true
			}
		}

		idx = tagEnd + end + len(scriptClose)
	}
}

// ---------------------------------------------------------------------------
// Content length computation.
// ---------------------------------------------------------------------------

// computeContentLengths scans the HTML to measure:
//   - visibleTextLen: non-space characters outside tags, scripts, styles, and <head>
//   - scriptContentLen: all bytes inside <script>…</script> content
//
// The scanner tracks which "zone" we're in (head, script, style, or body).
// In the body zone, `<` starts a tag and is skipped to the matching `>`.
// In script/style zones, everything until the closing tag is content.
// In the head zone, text is ignored (metadata, not visible content).
func computeContentLengths(html string, info *htmlAnalysis) {
	n := len(html)

	var (
		visibleTotal int
		scriptTotal  int
	)

	i := 0
	for i < n {
		// ---- head zone ----
		if strings.HasPrefix(html[i:], "<head") && (i+5 == n || isTagBoundary(html[i+5])) {
			// Skip to end of <head>.
			i += 5
			for i < n && html[i] != '>' {
				i++
			}
			if i < n {
				i++ // skip '>'
			}
			// Consume everything until </head>.
			end := strings.Index(html[i:], "</head>")
			if end < 0 {
				break
			}
			i += end + 7 // len("</head>")
			continue
		}

		// ---- style zone ----
		if strings.HasPrefix(html[i:], "<style") && (i+6 == n || isTagBoundary(html[i+6])) {
			i += 6
			for i < n && html[i] != '>' {
				i++
			}
			if i < n {
				i++
			}
			// Consume everything until </style>.
			end := strings.Index(html[i:], "</style>")
			if end < 0 {
				break
			}
			i += end + 8 // len("</style>")
			continue
		}

		// ---- script zone ----
		if strings.HasPrefix(html[i:], "<script") && (i+7 == n || isTagBoundary(html[i+7])) {
			i += 7
			for i < n && html[i] != '>' {
				i++
			}
			if i < n {
				i++
			}
			contentStart := i
			end := strings.Index(html[i:], "</script>")
			if end < 0 {
				// Unclosed script — count remaining as script content.
				scriptTotal += n - contentStart
				break
			}
			scriptTotal += end
			i += end + 9 // len("</script>")
			continue
		}

		// ---- body zone: skip tags ----
		if html[i] == '<' {
			// Skip past the tag (to the matching '>').
			j := i + 1
			for j < n && html[j] != '>' {
				j++
			}
			if j < n {
				i = j + 1 // skip past '>'
			} else {
				i = n
			}
			continue
		}

		// ---- body zone: visible text character ----
		ch := html[i]
		if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' && ch != '\f' {
			visibleTotal++
		}
		i++
	}

	info.visibleTextLen = visibleTotal
	info.scriptContentLen = scriptTotal
}

// isTagBoundary returns true for characters that can follow a tag name
// in an opening tag (space, >, /, newline, tab, etc.).
func isTagBoundary(ch byte) bool {
	return ch == ' ' || ch == '>' || ch == '/' || ch == '\t' ||
		ch == '\n' || ch == '\r' || ch == '\f'
}
