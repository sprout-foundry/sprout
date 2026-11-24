package agent

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// Memory prompt modes (config memory_prompt).
const (
	MemoryPromptFull  = "full"  // every body, up to the per-memory cap
	MemoryPromptIndex = "index" // names and first lines; bodies are read on demand with manage_memory
	MemoryPromptOff   = "off"   // nothing; manage_memory still lists, searches and reads them
)

const memoryIndexSummaryChars = 120

// normalizeMemoryPromptMode resolves a config memory_prompt value to one of the
// MemoryPrompt* constants. "" is the index default. ok is false for a value that
// names no mode; the caller then runs full and warns, so a typo never silently
// changes what the prompt carries.
func normalizeMemoryPromptMode(mode string) (normalized string, ok bool) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return MemoryPromptIndex, true
	}
	switch mode {
	case MemoryPromptFull, MemoryPromptIndex, MemoryPromptOff:
		return mode, true
	default:
		return MemoryPromptFull, false
	}
}

// LoadMemoriesForPromptMode formats the memories for the conversation's first user message in the given mode.
// maxBytes <= 0 uses maxMemoryPromptBytes. Every request carries the result, so the index
// mode (the default) keeps a large memory store from costing thousands of tokens a request.
//
// Rendering is deterministic and stable under edits: memories are sorted by name with a
// stable comparator (so directory-read order cannot drift the block), no timestamps, counts
// or other volatile text appear beyond the omission notice(s), and a fixed per-memory cap
// bounds each entry — a memory over the cap is omitted without moving the inclusion or
// ordering of any other memory. memory_prompt_max_bytes / maxBytes stays as the outer guard.
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
	// Explicit in the renderer: LoadAllMemories sorts too, but the block must not
	// depend on that order or on directory-read order.
	sort.SliceStable(memories, func(i, j int) bool {
		return memories[i].Name < memories[j].Name
	})

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
	omittedPerMemory := 0
	omittedOuter := 0
	for i, memory := range memories {
		var entry string
		if index {
			entry = fmt.Sprintf("- `%s`: %s\n", memory.Name, memorySummary(memory.Content))
		} else {
			entry = fmt.Sprintf("### %s\n%s\n\n", memory.Name, memoryBody(memory.Content))
		}
		if len(entry) > maxMemoryEntryBytes {
			omittedPerMemory++
			continue
		}
		if written+len(entry) > maxBytes {
			omittedOuter += len(memories) - i // every remaining entry: outer guard
			break
		}
		sb.WriteString(entry)
		written += len(entry)
	}
	if omittedPerMemory > 0 {
		fmt.Fprintf(&sb, "*[%d memory file(s) omitted — per-memory cap %d bytes]*\n\n", omittedPerMemory, maxMemoryEntryBytes)
	}
	if omittedOuter > 0 {
		fmt.Fprintf(&sb, "*[%d memory file(s) omitted — total guard %d bytes]*\n\n", omittedOuter, maxBytes)
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
