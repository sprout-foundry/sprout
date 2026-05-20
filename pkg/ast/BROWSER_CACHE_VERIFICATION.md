# BrowserCache Verification — SP-025-4c

## Executive Summary

**`browser_cache.go` does NOT persist compiled grammar binaries to browser storage.**
It persists **metadata only** (name, size, loadedAt) to `localStorage`. The actual
`*gotreesitter.Language` pointers remain **in-memory only** because they cannot be
serialized across page loads.

The "persistence" is a **hint system**: localStorage remembers which grammars were
loaded in a previous session, so on subsequent page loads the application knows
which grammars to reload via `PreloadCache()`.

## Storage Mechanism

| Aspect | Detail |
|--------|--------|
| **Storage API** | `localStorage` (NOT IndexedDB) |
| **Build tag** | `//go:build js && wasm` |
| **Data persisted** | JSON metadata: `{name, size, loadedAt}` |
| **Data NOT persisted** | `*gotreesitter.Language` pointer (binary grammar data) |
| **Key prefix** | `sprout:ast:grammar:` |
| **Max capacity** | ~5-10 MB (browser localStorage limit) — metadata is tiny, so this is not a constraint |

## How It Works

### First Page Load
```
1. InitBrowserCache() → creates new BrowserCache (empty in-memory map)
2. CachedGrammarNames() → returns nil (nothing in localStorage yet)
3. PreloadCache() → loads ALL SupportedLanguages, calls Put() for each
4. Put(blob) → stores *Language in-memory map + writes JSON metadata to localStorage
```

### Second Page Load (After Reload)
```
1. InitBrowserCache() → creates new BrowserCache (empty in-memory map)
2. CachedGrammarNames() → reads localStorage, returns ["go", "python", ...]
3. Caller decides what to preload (could be all, could be a subset)
4. PreloadCache() → reloads the same grammars from gotreesitter/grammars package
5. Get("go") → hits the in-memory cache (language was reloaded)
```

### Key Methods

| Method | What It Does | Build Dependency |
|--------|-------------|-----------------|
| `InitBrowserCache()` | Creates new BrowserCache, replaces default cache | `js && wasm` only (no-op stub otherwise) |
| `CachedGrammarNames()` | Reads localStorage, returns names of previously-cached grammars | `js && wasm` only (returns nil otherwise) |
| `Put(blob)` | Stores blob in-memory + writes metadata to localStorage | Only in WASM build |
| `Get(name)` | Returns from in-memory map; increments hit/miss counters | Same in all builds |
| `Invalidate(name)` | Removes from in-memory map + removes localStorage entry | Only localStorage part in WASM |
| `InvalidateAll()` | Clears in-memory map + clears all prefixed localStorage entries | Only localStorage part in WASM |

## What Is Persisted (localStorage Example)

```
Key:   sprout:ast:grammar:go
Value: {"name":"go","size":123456,"loadedAt":"2026-05-18T12:00:00Z"}

Key:   sprout:ast:grammar:python
Value: {"name":"python","size":98765,"loadedAt":"2026-05-18T12:00:01Z"}
```

## What Is NOT Persisted

- The `*gotreesitter.Language` pointer (contains parse tables, lex states, etc.)
- Any binary grammar data
- Hit/miss/eviction counters (these are atomic.Int64 counters, session-scoped)

## Manual Verification

### Prerequisites
- Go WASM toolchain installed (`GOOS=js GOARCH=wasm`)
- A local HTTP server (e.g., `python3 -m http.server 8080`)

### Steps

**1. Build the WASM binary**
```bash
cd cmd/wasm
GOOS=js GOARCH=wasm go build -o main.wasm
```

**2. Serve with the WASM polyfill**
```bash
# Clone TinyGo's or Go's wasm_exec.js if not already available
cp $(go env GOROOT)/misc/wasm/wasm_exec.js .
# Serve the directory
python3 -m http.server 8080
```

**3. Open browser DevTools → Application → Local Storage**
Navigate to `http://localhost:8080` and wait for the WASM module to load and
`PreloadCache()` to execute.

**4. Verify localStorage entries exist**
In the DevTools Application tab, under Local Storage → `http://localhost:8080`:
```
sprout:ast:grammar:go       {"name":"go","size":123456,"loadedAt":"2026-05-18T..."}
sprout:ast:grammar:python   {"name":"python","size":98765,"loadedAt":"2026-05-18T..."}
...
```

**5. Reload the page (F5 / Ctrl+R)**
- The in-memory map is destroyed (WASM restarts)
- localStorage entries persist in the browser

**6. Verify CachedGrammarNames() detects previous entries**
Add console logging in your WASM entry point:
```go
names := ast.CachedGrammarNames()
fmt.Printf("Cached grammars from previous session: %v\n", names)
```
You should see the grammar names printed, confirming localStorage was read
successfully.

**7. Verify Get() works after reload + PreloadCache()**
```go
ast.PreloadCache()
blob, ok := ast.DefaultCache().Get("go")
// ok should be true, blob.Language should not be nil
```

### Clearing the Cache

To reset: open DevTools Console and run:
```javascript
for (let i = localStorage.length - 1; i >= 0; i--) {
    const key = localStorage.key(i);
    if (key.startsWith('sprout:ast:grammar:')) {
        localStorage.removeItem(key);
    }
}
```
Or use the Application tab UI to remove entries.

## Why Not IndexedDB?

IndexedDB would be needed to persist the actual compiled grammar binary data
(typically 100KB-500KB per language). The current design deliberately avoids
this:

1. `*gotreesitter.Language` contains Go struct pointers that cannot be serialized
2. Re-loading from the `grammars` package is fast enough for most use cases
3. localStorage metadata (~200 bytes per grammar) is tiny and has no quota concerns
4. Keeping it simple avoids async storage complexity (IndexedDB is async-only)

If true cross-session persistence of compiled grammars is needed, the approach
would be:
- Serialize grammar binary data to IndexedDB (requires a custom serializer)
- On page load, read from IndexedDB and reconstruct `*Language` structs
- Fall back to `grammars` package if IndexedDB entry is missing

## Existing Test Coverage

| Test File | Coverage |
|-----------|----------|
| `browser_cache_test.go` | Non-WASM stubs, metadata JSON round-trip, MemoryCache contract |
| `cache_test.go` | MemoryCache CRUD, Stats, Names, PreloadCache, SetDefaultCache panic |

### Not Testable in Standard Go Tests

- Actual `localStorage` read/write (requires WASM runtime)
- Cross-page-load persistence (requires browser reload)
- JS interop panic recovery (requires WASM + error-triggering browser state)
- `CachedGrammarNames()` returning non-nil (requires pre-populated localStorage)

### Manual Test Checklist

- [ ] Build WASM binary and serve it
- [ ] First load: verify localStorage entries are created
- [ ] Reload: verify `CachedGrammarNames()` returns grammar names
- [ ] Reload + `PreloadCache()`: verify `Get()` returns valid Language pointers
- [ ] `Invalidate()`: verify localStorage entry is removed for that grammar
- [ ] `InvalidateAll()`: verify all prefixed localStorage entries are removed
- [ ] Quota exceeded: verify `Put()` doesn't panic (panic recovery)
- [ ] localStorage disabled: verify cache still works (no panic, in-memory only)

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                     Page Load (First Time)                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  InitBrowserCache() ──→ NewBrowserCache()                       │
│                         data = {} (empty map)                   │
│                                                                  │
│  CachedGrammarNames() ──→ [] (localStorage empty)               │
│                                                                  │
│  PreloadCache() ──→ For each SupportedLanguage:                 │
│    │                                                            │
│    ├─ grammars.DetectLanguageByName("go")                       │
│    ├─ entry.Language() ──→ *gotreesitter.Language               │
│    └─ Put(blob) ──→ data["go"] = &GrammarBlob{Language: ...}   │
│                         localStorage.setItem(                    │
│                           "sprout:ast:grammar:go",               │
│                           `{"name":"go","size":...}`)            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                  Page Load (After Reload)                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  InitBrowserCache() ──→ NewBrowserCache()                       │
│                         data = {} (empty map)                   │
│                                                                  │
│  CachedGrammarNames() ──→ ["go", "python", ...]                 │
│    (reads localStorage, parses JSON)                            │
│                                                                  │
│  PreloadCache() ──→ Reloads all SupportedLanguages              │
│    (or caller can filter to only names from CachedGrammarNames) │
│                                                                  │
│  Get("go") ──→ hits (Language was reloaded by PreloadCache)     │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## File Reference

| File | Build Tag | Purpose |
|------|-----------|---------|
| `browser_cache.go` | `js && wasm` | BrowserCache with localStorage persistence |
| `browser_cache_stub.go` | `!(js && wasm)` | No-op InitBrowserCache/CachedGrammarNames stubs |
| `browser_cache_test.go` | none | Stub tests + metadata serialization tests |
| `cache.go` | none | GrammarCache interface, MemoryCache, PreloadCache, EstimateLanguageSize |
| `cache_test.go` | none | MemoryCache functional tests |
