//go:build js

package main

// binary_funcs.go — byte-exact file I/O for the WASM shell. readFile returns
// text through JSON, which mangles images; these return and accept
// Uint8Array so images survive the trip between the VFS and the page.

import (
	"os"
	"path/filepath"
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/console"
)

func binaryJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"readFileBytes": js.FuncOf(readFileBytesFunc),
		"saveImage":     js.FuncOf(saveImageFunc),
	}
}

func bytesToJS(data []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(arr, data)
	return arr
}

// readFileBytesFunc returns {bytes: Uint8Array} or {error: string}.
// args[0] (string) — workspace-relative or absolute path.
func readFileBytesFunc(_ js.Value, args []js.Value) interface{} {
	path := argString(args, 0, "")
	if path == "" {
		return map[string]interface{}{"error": "readFileBytes: missing path argument"}
	}
	data, err := os.ReadFile(workspacePath(path))
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	return map[string]interface{}{"bytes": bytesToJS(data)}
}

// saveImageFunc stores an uploaded image the way the daemon's
// /api/upload/image does and returns {path, filename} or {error}.
// args[0] (Uint8Array | ArrayBuffer) — the image bytes.
func saveImageFunc(_ js.Value, args []js.Value) interface{} {
	data, err := copyBytesFromJS(args, 0)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	if ext, _ := console.DetectImageMagic(data); ext == "" {
		return map[string]interface{}{"error": "Not a recognized image format"}
	}
	saved, err := console.SavePastedImage(data, workspaceRoot)
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}
	if !filepath.IsAbs(saved) {
		saved = filepath.Join(workspaceRoot, saved)
	}
	return map[string]interface{}{"path": saved, "filename": filepath.Base(saved)}
}
