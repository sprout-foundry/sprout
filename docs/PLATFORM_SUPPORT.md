# Platform Support

Sprout targets five build platforms. Most features work everywhere; some
are inherently platform-specific (process management, terminal control,
native embeddings). This matrix documents what's available on each.

## Build targets

| Target | Build command | Usage |
|--------|---------------|-------|
| **Linux/macOS (native)** | `go build .` | CLI daemon, WebUI server, full agent |
| **Windows** | `go build .` | CLI daemon, WebUI server, full agent |
| **WASM** | `make build-all` (`GOOS=js GOARCH=wasm`) | Browser shell, WebUI embedded agent |
| **no-CGO** | `go build -tags '!cgo'` | Stripped binary without ONNX embeddings |
| **Browser (rod)** | `go build .` | Requires Chromium for headless browser features |

## Feature availability matrix

| Feature | Linux/macOS | Windows | WASM | no-CGO |
|---------|:-----------:|:-------:|:----:|:------:|
| **Shell execution** | ✅ Full | ✅ Full | ⚠️ No streaming | ✅ Full |
| **Background processes** | ✅ Process groups | ✅ Job Objects (cascade kill) | ❌ Not available | ✅ Full |
| **Process signals** | ✅ SIGINT→TERM→KILL | ⚠️ Kill only | ❌ N/A | ✅ Full |
| **Vision / image analysis** | ✅ Full | ✅ Full | ❌ Not available | ✅ Full |
| **PDF processing** | ✅ Full | ✅ Full | ❌ Not available | ✅ Full |
| **Headless browser** | ✅ Full (rod) | ✅ Full (rod) | ❌ Not available | ✅ Full |
| **Code intelligence graph** | ✅ SQLite store | ✅ SQLite store | ❌ Not available | ✅ Full |
| **ONNX embeddings** | ✅ Full (CGO) | ✅ Full (CGO) | ⚠️ JS bridge | ❌ Static fallback |
| **Static embeddings** | ❌ Uses ONNX | ❌ Uses ONNX | ✅ JS-native | ✅ Hash fallback |
| **Terminal raw mode** | ✅ ioctl (OPOST safe) | ⚠️ term.MakeRaw | ❌ N/A | ✅ Full |
| **Terminal health** | ✅ Termios capture | ⚠️ Always "sane" | ❌ N/A | ✅ Full |
| **Signal handling** | ✅ Full set | ⚠️ Interrupt only | ⚠️ Interrupt only | ✅ Full |
| **OOM watchdog** | ✅ /proc scan | ❌ No-op | ❌ No-op | ✅ Full |
| **Automate sessions** | ✅ PID tracking | ✅ PID tracking | ❌ Not available | ✅ Full |
| **Foreground app detection** | ✅ osascript/xdotool | ❌ Not available | ❌ N/A | ✅ Full |
| **Panic key chord** | ✅ CGEvent/xrecord | ❌ Not available | ❌ N/A | ✅ Full |
| **Computer use** | ✅ Full | ⚠️ No process groups | ❌ N/A | ✅ Full |
| **Structured file tools** | ✅ Full | ✅ Full | ✅ Full | ✅ Full |
| **Memory / settings** | ✅ Full | ✅ Full | ✅ Full | ✅ Full |
| **Git operations** | ✅ Full | ✅ Full | ❌ No filesystem | ✅ Full |
| **Subagent spawning** | ✅ Full | ✅ Full | ✅ Full | ✅ Full |

Legend: ✅ Full support · ⚠️ Degraded but functional · ❌ Not available

## Known platform limitations

**WASM (browser):** Cannot spawn processes, access the filesystem directly,
or run native libraries. Shell commands route through a JS executor
registered by the WebUI. Vision, codegraph, and background processes return
informative errors. Terminal is hardcoded to 80×24.

**Windows:** Background processes are joined to a Job Object with
`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, so cleanup cascades to descendants
(unix-style process groups don't exist on Windows; SP-112-1). Signal
escalation uses `CTRL_BREAK_EVENT` → `TerminateProcess` instead of
SIGINT → SIGTERM → SIGKILL. Terminal raw mode uses `term.MakeRaw` (may
cause staircase rendering without OPOST preservation). PID-alive checks
use `OpenProcess` + `GetExitCodeProcess` (the legacy `os.FindProcess`
returned false positives for recycled PIDs).

**no-CGO:** ONNX Runtime requires CGO. Without it, embeddings fall back to
a deterministic hash-based provider (384-dim FNV-1a). Search and recall
work but with lower quality than ONNX models.

**non-Linux:** OOM watchdog has no `/proc` to scan, so memory alerts never
fire. Process start-time comparison (for PID-reuse detection) fail-opens.

Full audit details: see [SP-112](../roadmap/SP-112-platform-parity.md).
