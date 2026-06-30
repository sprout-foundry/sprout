# CLI Streaming Word-Erasure Bug — Diagnosis

## Symptom (user report)

> i am still getting a lot of words and content in the cli replaced right after it has been streamed. some of it shows up later in a nice overview, but some doesn't. ... also, any content between tool calls seems to be totally lost. each word comes through, then gets erased and so on.

Reproduced locally: words show up one chunk at a time; the word you just saw is gone a moment later; the final turn only shows the last `physicalLines` of the entire response; earlier chunks are lost from the terminal scrollback.

## Root cause (verified with a focused test)

The subscriber goroutine in `cmd/agent_terminal_subscriber.go` writes directly
to `os.Stdout` without going through either the OutputRouter OR the
console-level `LockOutput()`. The renderer's `WriteChunk` writes to
stdout under `LockOutput()`, but stdout's byte stream is still
shared.

Concrete race (demonstrated by my repro test):

```go
// Renderer goroutine
for _, ch := range "The quick brown fox jumps.\n" {
    fmt.Print(string(ch)) // one byte at a time under outputMu
    time.Sleep(5ms)
}

// Subscriber goroutine (concurrent)
for i := 0; i < 10; i++ {
    fmt.Fprintln(os.Stdout) // \n, NO outputMu
    time.Sleep(2ms)
}
```

Resulting bytes on stdout:
```
"\n  The \n\nquick \n\n\nbrown \n\nfox \n\njumps.\n"
```

Renderer's internal state after this:
```
physicalLines = 1     <-- WRONG (only counted the trailing \n)
seg = "The quick brown fox jumps.\n"
atLineStart = true
curLineRunes = 0
```

What the user actually sees on the terminal:
```
The
quick
brown
fox
jumps.
```

(five broken rows, with the renderer's `  ` indent missing from each
because the chunks were "The", " quick", " brown"... but every
subscriber \n splits them).

What `FinalizeAtTurnEnd` then does:
- Computes `upRows = physicalLines = 1`
- Walks back 1 row from cursor → clears the LAST row ("jumps.")
- Prints the formatted "The quick brown fox jumps." in its place
- Leaves rows 1..4 untouched

User-visible end state:
```
The
quick
brown
fox
The quick brown fox jumps.
```

So 4 of 5 words "vanish" from the screen and the formatted output
duplicates them. **This is exactly the user's symptom**: each word
appears momentarily, then everything except the last one vanishes
behind formatter-induced duplication that almost looks like erasure.

## Where the bytes leak in

`cmd/agent_terminal_subscriber.go` writes to stdout directly (no
console lock) at these sites:

| Line | Event | Write |
|------|-------|-------|
| 136 | `EventTypeToolStart` | `fmt.Fprintln(os.Stdout)` (blank line) |
| 218 | `EventTypeSubagentActivity completed/cancelled` | `fmt.Fprintln(os.Stderr, ...)` (OK — stderr is independent) |
| 305 | `EventTypeTodoUpdate` | `fmt.Fprintln(os.Stdout, console.GlyphInfo.Prefix()+"Todo list cleared")` |
| 307 | `EventTypeTodoUpdate` | `fmt.Fprintln(os.Stdout, formatTodoListBlock(todosRaw))` — multi-line block |

Lines 136, 305, 307 are the offenders: every tool start between prose
chunks leaves a blank line on stdout that `WriteChunk` later sees as
already-spent progress.

The renderer's `WriteChunk` only counts `\n` it emitted itself; the
borrower's blank lines are invisible to its `physicalLines` /
`atLineStart` accounting, so when `FinalizeAtTurnEnd` walks back, it
walks too few rows and reprints the formatted version in the wrong
place.

## Fix

Three coordinated changes — none of them is risky individually, and
together they make the symptom disappear without regressing the
existing "Indent" / "FinalizeAtTurnEnd" / "CursorOnFreshRow" work in
`pkg/console/assistant_turn_renderer.go`:

### 1. Serialize subscriber stdout writes through the console lock

`cmd/agent_terminal_subscriber.go` already imports `pkg/console`. Wrap
each offending stdout write in `console.LockOutput()` /
`console.UnlockOutput()` so the renderer's `WriteChunk` can't be
mid-rune-iteration when the blank lines arrive. The blank lines still
land, but the renderer's chunk write is atomic w.r.t. them.

### 2. Notify the renderer of external writes via the hook

The OutputRouter has a `SetExternalWriteHook(r.OnExternalWrite)` API
that the renderer already exposes. The subscriber currently bypasses it
for the writes in question. The cleanest fix is to route ALL
mid-turn stdout writes through the OutputRouter — `RouteTerminalOnly`
or `RouteAgentMessage` — so the hook fires automatically. Subscribers
can pull the router via `chatAgent.OutputRouter()` (same accessor the
renderer uses).

### 3. Reconcile `physicalLines` after external writes

Even with the console lock fix, the subscriber's `\n` advances the
cursor — the renderer's `physicalLines` lags by N. The current
`OnExternalWrite` resets `seg` but doesn't touch `physicalLines` /
`atLineStart` / `curLineRunes`. Add a parameter (or expose a counter
the subscriber can increment) so `OnExternalWrite` knows how many rows
the external write consumed and bumps `physicalLines` accordingly.

Alternatively — and simpler — change `WriteChunk` so that
`atLineStart=true` is the ONLY way a chunk starts; have the
subscriber's blank-line write trigger `OnExternalWrite` with the row
delta, which:
- clears the segment buffer (already does),
- sets `physicalLines += externalRowCount`,
- leaves `atLineStart`, `curLineRunes` as the renderer already had
  them (the external write reset cursor to col 0 of a fresh row).

Then `FinalizeAtTurnEnd`'s `upRows = physicalLines + (partial line
correction)` walks back the actual number of rows.

## Tests

Add three regression tests:

1. **`TestSubscriberAndRendererCoordinate`** — fire a flow that
   interleaves `Render.WriteChunk` calls with concurrent `fmt.Fprintln`
   against `os.Stdout` (locked). Verify the resulting buffer matches
   the renderer's `physicalLines` count, and that
   `FinalizeAtTurnEnd` clears exactly `physicalLines` rows.

2. **`TestOnExternalWriteBumpsPhysicalLines`** — drive the renderer
   through:
   ```
   writeChunk("The ")
   OnExternalWrite(rows=1)   // blank line emitted externally
   writeChunk("quick\n")
   FinalizeAtTurnEnd()
   ```
   Confirm physicalLines == 2 throughout the second chunk and the
   finalizer walks back exactly 2 rows.

3. **`TestToolSubscriberSeqNoErase`** — drive the tool subscriber
   through a real ToolStart → ToolEnd sequence and confirm the row
   math matches what the renderer thinks.

## Out of scope (DO NOT TOUCH)

- `pkg/agent/output_router.go:325` (`\r\033[K` for non-streaming
  fallback) — that path doesn't fire when streaming is enabled; not
  the cause.
- `pkg/console/activity_indicator.go` — the renderer's `WriteChunk`
  correctly calls `LockOutput`; the indicator's `\r\033[K` clears
  only its own row. Verified.
- `pkg/console/assistant_turn_renderer.go::FinalizeAtTurnEnd` math —
  the math is sound given correct `physicalLines`. Fix is upstream.
