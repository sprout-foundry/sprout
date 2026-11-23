package agent

import (
	"fmt"
	"log"
	"strings"
)

// Memory prompt modes (config memory_prompt).
const (
	MemoryPromptFull  = "full"  // every body, up to the byte cap (the default)
	MemoryPromptIndex = "index" // names and first lines; bodies are read on demand with manage_memory
	MemoryPromptOff   = "off"   // nothing; manage_memory still lists, searches and reads them
)

const memoryIndexSummaryChars = 120

// normalizeMemoryPromptMode resolves a config memory_prompt value to one of the
// MemoryPrompt* constants. "" is the full default. ok is false for a value that
// names no mode; the caller then runs full and warns, so a typo never silently
// changes what the prompt carries.
func normalizeMemoryPromptMode(mode string) (normalized string, ok bool) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return MemoryPromptFull, true
	}
	switch mode {
	case MemoryPromptFull, MemoryPromptIndex, MemoryPromptOff:
		return mode, true
	default:
		return MemoryPromptFull, false
	}
}

// LoadMemoriesForPromptMode formats the memories for the system prompt in the given mode.
// maxBytes <= 0 uses maxMemoryPromptBytes. Every request carries the result, so the index
// mode keeps a large memory store from costing thousands of tokens a request.
func LoadMemoriesForPromptMode(mode string, maxBytes int) string {
	normalized, ok := normalizeMemoryPromptMode(mode)
	if !ok {
		log.Printf("[config] warning: unknown memory_prompt %q; using \"full\" (want \"full\", \"index\", or \"off\")", mode)
	}
	mode = normalized
	if mode == MemoryPromptOff {
		return ""
	}
	if maxBytes <= 0 {
		maxBytes = maxMemoryPromptBytes
	}
	memories, err := LoadAllMemories()
	if err != nil || len(memories) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n---\n\n## Memories\n\n")
	index := mode == MemoryPromptIndex
	if index {
		sb.WriteString("Memories from previous sessions, by name. Read one with `manage_memory` " +
			"(operation \"read\", the name) when it bears on the task; search them with operation \"search\".\n\n")
	} else {
		sb.WriteString("The following memories capture user preferences and learned patterns from previous sessions. Use them to guide your behavior.\n\n")
	}

	written := 0
	for i, memory := range memories {
		var entry string
		if index {
			entry = fmt.Sprintf("- `%s`: %s\n", memory.Name, memorySummary(memory.Content))
		} else {
			entry = fmt.Sprintf("### %s\n%s\n\n", memory.Name, memoryBody(memory.Content))
		}
		if written+len(entry) > maxBytes {
			fmt.Fprintf(&sb, "*[%d additional memory file(s) omitted — total size exceeded %d bytes]*\n\n",
				len(memories)-i, maxBytes)
			break
		}
		sb.WriteString(entry)
		written += len(entry)
	}
	return sb.String()
}

// memoryBody drops a leading H1 title.
func memoryBody(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "# ") {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// memorySummary is the first non-empty line that is not a heading or front matter, shortened.
func memorySummary(content string) string {
	inFront := false
	for i, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if t == "---" && (i == 0 || inFront) {
			inFront = !inFront
			continue
		}
		if t == "" || inFront || strings.HasPrefix(t, "#") {
			continue
		}
		if r := []rune(t); len(r) > memoryIndexSummaryChars {
			return string(r[:memoryIndexSummaryChars]) + "…"
		}
		return t
	}
	return "(no summary)"
}
