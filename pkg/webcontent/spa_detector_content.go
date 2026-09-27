package webcontent

// spa_detector_content.go — content-length computation for SPA detection,
// split out of spa_detector.go. computeContentLengths is the single-pass
// scanner that measures visible-text and inline-script byte counts feeding
// htmlAnalysis; isTagBoundary identifies the characters that can follow an
// HTML tag name.
import "strings"

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
