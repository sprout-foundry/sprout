# SP-018: Memory System

**Status:** ✅ Implemented  
**Location:** `pkg/agent/memory.go`, `pkg/agent/memory_handlers.go`  
**Size:** ~360 lines Go implementation  
**Test Files:** 3 test files (1,102 lines total)

## Problem

The agent lacks persistent memory across conversations. Each session starts fresh, even when the user has explicitly stated preferences, conventions, or learned patterns that should apply to all future work. This leads to:

1. **Repetitive instructions** — Users must re-state project conventions ("use snake_case for database fields", "write tests before implementation") in every session.
2. **Lost context** — Preferences and patterns learned during one conversation disappear on reset or in the next session.
3. **Inconsistent behavior** — The agent doesn't maintain consistency across sessions without persistent guidance.

The agent already has conversation history persistence (messages, checkpoints, summaries), but this is session-scoped and reset on new conversations. A separate, user-controlled memory store is needed for preferences that should persist across all conversations.

## Current State

The Memory System provides persistent cross-conversation memory that survives session resets. Users can save and retrieve preferences, learned patterns, project conventions, and any useful information that should apply to all future conversations.

### Architecture

```
User Input → Memory Tool Call
                │
                ├──► add_memory(name, content)
                │         └──► SaveMemory() → sanitizeMemoryName() → write .md file
                │
                ├──► read_memory(name)
                │         └──► LoadMemoryContent() → read .md file → return content
                │
                ├──► list_memories()
                │         └──► LoadAllMemories() → read all .md → return list
                │
                └──► delete_memory(name)
                          └──► DeleteMemory() → remove .md file

Agent Initialization
        │
        └──► LoadMemoriesForPrompt() → LoadAllMemories() → format as ## Memories section
                                       └──► Injected into system prompt for ALL conversations
```

### Tools

| Tool | Handler | Parameters | Description |
|------|---------|------------|-------------|
| `add_memory` | `handleAddMemory` | `name` (string, required), `content` (string, required) | Creates or overwrites a memory file. Name is sanitized before saving. |
| `read_memory` | `handleReadMemory` | `name` (string, required) | Reads and returns the full markdown content of a specific memory. |
| `list_memories` | `handleListMemories` | (none) | Lists all saved memories with names and first-line titles (alphabetically sorted). |
| `delete_memory` | `handleDeleteMemory` | `name` (string, required) | Deletes a memory file by name. Strips `.md` extension if provided. |

### Storage

Memories are stored as markdown files in the memories directory:

```
~/.config/sprout/memories/
├── git-safety.md
├── test-conventions.md
├── code-style.md
└── project-goals.md
```

**Compatibility:** The memory directory path is resolved via `configuration.GetConfigDir()` which respects `XDG_CONFIG_HOME` when set, defaulting to `~/.config/sprout/memories/`.

**File format:** Plain markdown files. The agent reads and writes these files directly; users can also edit them manually in a text editor.

**Naming:** Memory files use sanitized names derived from the user-provided name:
- Converted to lowercase
- Spaces replaced with hyphens
- Special characters stripped (keeps only alphanumeric, hyphens, underscores)
- `.md` extension appended automatically

**Examples:**
- Input: `"Git Safety Rules"` → File: `git-safety-rules.md`
- Input: `"my memory!"` → File: `my-memory.md`
- Input: `"Preferences for Go"` → File: `preferences-for-go.md`

### System Prompt Integration

Memories are automatically loaded into the system prompt during agent initialization via `LoadMemoriesForPrompt()`.

**Format:**

```markdown
---

## Memories

The following memories capture user preferences and learned patterns from previous sessions. Use them to guide your behavior.

### git-safety

Always run `git status` before and after operations. Never use `--force` without explicit confirmation.

### test-conventions

Write tests before implementation. Use table-driven tests where applicable. Aim for >80% coverage.

### code-style

Use snake_case for database fields, camelCase for Go structs. Keep functions under 50 lines.
```

**H1 title stripping:** If a memory file starts with an H1 heading (e.g., `# My Title`), that line is stripped from the content to avoid duplication with the `### memory-name` section header.

**Size limit:** Total memory content is capped at `maxMemoryPromptBytes = 50_000` (~50KB). This prevents individual oversized memory files from consuming the entire context window before conversation messages are added (~12,500 tokens at 4 chars/token). When the cap is exceeded, a truncation notice is appended:

```markdown
*[3 additional memory file(s) omitted — total size exceeded 50000 bytes]*
```

**Zero overhead:** If no memories exist, `LoadMemoriesForPrompt()` returns an empty string. No special handling is needed in the agent code.

### Core Functions

| Function | File | Purpose |
|----------|------|---------|
| `LoadAllMemories()` | `memory.go` | Reads all `.md` files from memories directory, returns sorted `[]MemoryInfo` with name, path, and content |
| `LoadMemoryContent(name)` | `memory.go` | Reads a single memory file by name (without `.md` extension) |
| `SaveMemory(name, content)` | `memory.go` | Writes a memory file with name sanitization |
| `DeleteMemory(name)` | `memory.go` | Deletes a memory file; accepts name with or without `.md` extension |
| `ListMemories()` | `memory.go` | Returns memories with names and first-line titles (extracted from content) |
| `LoadMemoriesForPrompt()` | `memory.go` | Loads all memories and formats for system prompt injection with 50KB cap |
| `sanitizeMemoryName(name)` | `memory.go` | Sanitizes memory names for safe filenames |
| `getMemoryDir()` | `memory.go` | Returns path to memories directory, creates if missing |

### Tool Handlers

| Handler | Purpose |
|---------|---------|
| `handleAddMemory()` | Validates args, calls `SaveMemory()`, returns confirmation message |
| `handleReadMemory()` | Validates args, calls `LoadMemoryContent()`, returns formatted content |
| `handleListMemories()` | Calls `ListMemories()`, formats as bulleted list |
| `handleDeleteMemory()` | Validates args, strips `.md` extension, calls `DeleteMemory()` |

## Design Decisions

### Markdown Format

Memories are stored as plain markdown files for maximum simplicity and transparency.

**Rationale:**
- Users can edit memories directly in a text editor
- Human-readable format enables easy inspection and debugging
- Markdown supports rich formatting (headings, lists, code blocks) for structured preferences
- No database or serialization overhead

### 50KB Total Cap

`maxMemoryPromptBytes = 50_000` limits the total size of all memories injected into the system prompt.

**Rationale:**
- Prevents context window exhaustion before conversation begins
- At ~4 bytes/token, 50KB ≈ 12,500 tokens — leaves room for actual conversation
- Truncation notice makes the cap visible to users
- Users can manage memory size by editing or deleting large files

### Auto-Loading

Memories are loaded automatically during agent initialization, injected into the system prompt for **all** conversations.

**Rationale:**
- Zero friction — users don't need to manually load memories each session
- Consistent behavior — preferences always apply
- No opt-in required — the system just works

### Name Sanitization

`sanitizeMemoryName()` enforces safe filenames:
- Lowercase
- Spaces → hyphens
- Strip special characters (keep only `[a-z0-9\-_]`)
- Remove leading/trailing hyphens and underscores
- Default to `"untitled"` if empty after sanitization

**Rationale:**
- Cross-platform compatibility
- Prevents path traversal and filesystem errors
- Predictable file naming
- User-friendly CLI references (e.g., `read_memory git-safety`)

### H1 Title Stripping

`LoadMemoriesForPrompt()` strips leading H1 headings from memory content.

**Rationale:**
- Avoids duplicate headers when memory content starts with `# My Title`
- The section header (`### memory-name`) already provides identification
- Cleaner output in the system prompt

## Key Files

| File | Lines | Purpose |
|------|-------|---------|
| `pkg/agent/memory.go` | 271 | Core memory functions: load, save, delete, list, prompt formatting |
| `pkg/agent/memory_handlers.go` | 89 | Tool handlers for add/read/list/delete |
| `pkg/agent/tool_definitions.go` | 4 registrations | Tool registry entries for the 4 memory tools |
| `pkg/agent/memory_test.go` | 595 | Tests for core memory functions |
| `pkg/agent/memory_handlers_test.go` | 303 | Tests for tool handlers |
| `pkg/agent/memory_handlers_new_test.go` | 204 | Additional handler tests and parameter validation |

## Test Coverage

**Core functions (memory_test.go):**
- `TestSanitizeMemoryName` — 21 test cases covering normal cases, special character stripping, edge cases, real-world memory names
- `TestSaveMemory_AndLoad` — Save and load cycle
- `TestSaveMemory_SanitizesName` — Name sanitization on save
- `TestSaveMemory_OverwritesExisting` — Overwrite behavior
- `TestLoadMemoryContent_NonExistent` — Error handling
- `TestDeleteMemory` — Delete and verify
- `TestLoadAllMemories_Empty` / `WithFiles` — Empty and populated directory
- `TestLoadAllMemories_IgnoresNonMdFiles` — Only `.md` files processed
- `TestListMemories_FirstNonEmptyLine` — Title extraction
- `TestLoadMemoriesForPrompt_WithMemories` / `SkipsLeadingH1` / `RespectsMaxBytes` — Prompt formatting

**Tool handlers (memory_handlers_test.go, memory_handlers_new_test.go):**
- `TestHandleAddMemoryMissingArgs` / `Success` / `Sanitized` — Parameter validation and behavior
- `TestHandleReadMemoryMissingArgs` / `NotFound` / `Success` — Read tool behavior
- `TestHandleListMemoriesEmpty` / `Success` / `Truncation` — List tool behavior
- `TestHandleDeleteMemoryMissingArgs` / `NotFound` / `Success` / `WithSuffix` — Delete tool behavior

## Success Criteria

| Metric | Target | Actual |
|--------|--------|--------|
| Persistent storage across sessions | ✅ Memories survive session resets | ✅ Implemented |
| Zero overhead when not used | ✅ Empty string if no memories | ✅ Implemented |
| Auto-loading into system prompt | ✅ Loaded during agent init | ✅ Implemented |
| 50KB cap enforcement | ✅ Truncation notice when exceeded | ✅ Implemented |
| Name sanitization | ✅ Cross-platform safe filenames | ✅ Implemented |
| Manual editing support | ✅ Users can edit `.md` files directly | ✅ Implemented |
| Test coverage | >80% | ✅ ~1,100 lines of tests |

## Open Questions

None — the feature is fully implemented and tested.

## Future Enhancements

**Potential improvements (not currently planned):**

1. **Memory categories/tags** — Add optional tags to memories for selective loading (e.g., only load `git`-tagged memories when working on git operations).
2. **Memory priority** — Allow users to mark certain memories as higher priority for more prominent placement in the system prompt.
3. **Memory templates** — Provide pre-built memory templates for common use cases (e.g., "Go conventions", "test patterns").
4. **Memory expiration** — Add optional expiration dates for time-limited memories (e.g., project-specific preferences for a specific sprint).
5. **Memory sharing** — Allow exporting/importing memory sets for team conventions.
