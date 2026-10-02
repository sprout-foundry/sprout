//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"syscall/js"

	"github.com/sprout-foundry/sprout/pkg/wasmshell"
)

func main() {
	// Initialize the shell environment.
	wasmshell.SetShellEnv(wasmshell.NewEnv())
	store = newStore()

	// The shell's home holds its config; the project gets its own directory.
	home := wasmshell.ShellEnv.Get("HOME")
	os.MkdirAll(home, 0755)
	_ = setWorkspaceRoot(workspaceRoot)

	// Plug our IndexedDB store into the wasmshell package.
	wasmshell.SetStoreWriter(store)

	// Register the SproutWasm global object with all exposed functions.
	// Start with the shell-level API; feature areas add their entries from
	// their own *_funcs.go files.
	apiSurface := map[string]interface{}{
		"init":           js.FuncOf(initFunc),
		"executeCommand": js.FuncOf(executeCommandFunc),
		// Async variant for the interactive terminal: commands backed by JS
		// Promises (git via isomorphic-git) must not block the event loop.
		"executeCommandAsync": js.FuncOf(executeCommandAsyncFunc),
		"autoComplete":        js.FuncOf(autoCompleteFunc),
		"getCwd":              js.FuncOf(getCwdFunc),
		"getWorkspaceRoot":    js.FuncOf(getWorkspaceRootFunc),
		"changeDir":           js.FuncOf(changeDirFunc),
		"writeFile":           js.FuncOf(writeFileFunc),
		"readFile":            js.FuncOf(readFileFunc),
		"listDir":             js.FuncOf(listDirFunc),
		"deleteFile":          js.FuncOf(deleteFileFunc),
		"getHistory":          js.FuncOf(getHistoryFunc),
		"getEnv":              js.FuncOf(getEnvFunc),
	}
	for name, fn := range configJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range syncJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range proxyJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range chatJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range agentJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range llmJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range credentialJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range astJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range toolExecJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range askUserJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range editApprovalJSFuncs() {
		apiSurface[name] = fn
	}
	for name, fn := range shellApprovalJSFuncs() {
		apiSurface[name] = fn
	}

	js.Global().Set("SproutWasm", js.ValueOf(apiSurface))

	// Block forever so the WASM module stays alive.
	c := make(chan struct{}, 0)
	<-c
}

// ─── JS Bridge Functions ────────────────────────────────────────────────

// initFunc initializes the WASM module. JS must set window.__sproutStore
// before calling this. Returns an error string (empty on success).
func initFunc(this js.Value, args []js.Value) interface{} {
	if len(args) > 0 {
		cfg := args[0]
		if cfg.Type() == js.TypeObject {
			homeKey := cfg.Get("home")
			if homeKey.Type() == js.TypeString {
				h := homeKey.String()
				os.MkdirAll(h, 0755)
				wasmshell.ShellEnv.Set("HOME", h)
			}
			if ws := cfg.Get("workspace"); ws.Type() == js.TypeString {
				if err := setWorkspaceRoot(ws.String()); err != nil {
					return "init: workspace: " + err.Error()
				}
			}
		}
	}

	errMsg := store.initStore()
	return errMsg
}

// executeCommandFunc executes a shell command string and returns JSON result.
func executeCommandFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return wasmshell.JSONResult(wasmshell.CmdResult{
			Stderr:   "executeCommand: missing argument\n",
			ExitCode: 1,
		})
	}

	input := args[0].String()
	result := wasmshell.ParseAndExecute(input)
	return wasmshell.JSONResult(result)
}

// executeCommandAsyncFunc runs the command on its own goroutine and
// resolves with the same JSON as executeCommand. Callers blocked on a JS
// Promise (browser git) deadlock on the synchronous path because the JS
// event loop cannot run while a js.FuncOf handler blocks.
func executeCommandAsyncFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return wasmshell.JSONResult(wasmshell.CmdResult{
			Stderr:   "executeCommandAsync: missing argument\n",
			ExitCode: 1,
		})
	}
	input := args[0].String()
	return asPromiseWithTimeout(0, func(_ context.Context) (interface{}, error) {
		return wasmshell.JSONResult(wasmshell.ParseAndExecute(input)), nil
	})
}

// autoCompleteFunc performs tab completion on the input.
func autoCompleteFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return "{}"
	}
	input := args[0].String()
	return wasmshell.AutoCompleteJSON(input)
}

// getCwdFunc returns the current working directory.
func getCwdFunc(this js.Value, args []js.Value) interface{} {
	cwd, err := os.Getwd()
	if err != nil {
		return wasmshell.ShellEnv.Get("PWD")
	}
	return cwd
}

// getWorkspaceRootFunc returns the project directory the host's paths are
// relative to.
func getWorkspaceRootFunc(this js.Value, args []js.Value) interface{} {
	return workspaceRoot
}

// changeDirFunc makes a directory the workspace: the host's paths, the
// agent and the shell all work from it.
func changeDirFunc(this js.Value, args []js.Value) interface{} {
	type result struct {
		CWD   string `json:"cwd"`
		Error string `json:"error"`
	}

	if len(args) < 1 {
		r := result{Error: "changeDir: missing argument"}
		data, _ := json.Marshal(r)
		return string(data)
	}

	dir := args[0].String()
	target := workspacePath(dir)
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		r := result{Error: fmt.Sprintf("cd: %s: No such directory", dir)}
		data, _ := json.Marshal(r)
		return string(data)
	}

	if err := setWorkspaceRoot(target); err != nil {
		r := result{Error: fmt.Sprintf("cd: %s: %s", dir, err.Error())}
		data, _ := json.Marshal(r)
		return string(data)
	}

	r := result{CWD: workspaceRoot}
	data, _ := json.Marshal(r)
	return string(data)
}

// writeFileFunc writes content to a file.
func writeFileFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return "writeFile: requires path and content arguments"
	}

	path := args[0].String()
	content := args[1].String()

	if err := wasmshell.SyncWriteFile(workspacePath(path), content); err != nil {
		return err.Error()
	}
	return ""
}

// readFileFunc reads a file's content.
func readFileFunc(this js.Value, args []js.Value) interface{} {
	type result struct {
		Content string `json:"content"`
		Error   string `json:"error"`
	}

	if len(args) < 1 {
		r := result{Error: "readFile: missing path argument"}
		data, _ := json.Marshal(r)
		return string(data)
	}

	path := args[0].String()
	content, err := wasmshell.ReadFileContent(workspacePath(path))
	if err != nil {
		r := result{Error: err.Error()}
		data, _ := json.Marshal(r)
		return string(data)
	}

	r := result{Content: content}
	data, _ := json.Marshal(r)
	return string(data)
}

// listDirFunc lists directory entries.
func listDirFunc(this js.Value, args []js.Value) interface{} {
	path := "."
	if len(args) > 0 {
		path = args[0].String()
	}

	jsonStr, err := wasmshell.ListDirEntryJSON(workspacePath(path))
	if err != nil {
		type result struct {
			Error string `json:"error"`
		}
		r := result{Error: err.Error()}
		data, _ := json.Marshal(r)
		return string(data)
	}

	return jsonStr
}

// deleteFileFunc deletes a file.
func deleteFileFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return "deleteFile: missing path argument"
	}

	path := args[0].String()
	if err := wasmshell.DeleteFilePath(workspacePath(path)); err != nil {
		return err.Error()
	}
	return ""
}

// getHistoryFunc returns the command history as JSON array.
func getHistoryFunc(this js.Value, args []js.Value) interface{} {
	// History is internal to wasmshell; we expose it via JSON result
	data, _ := json.Marshal([]string{})
	return string(data)
}

// getEnvFunc returns all environment variables as JSON object.
func getEnvFunc(this js.Value, args []js.Value) interface{} {
	data, _ := json.Marshal(wasmshell.ShellEnv.All())
	return string(data)
}
