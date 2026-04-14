//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall/js"
)

func main() {
	// Initialize the shell environment.
	shellEnv = newEnv()
	store = newStore()

	// Set up the home directory in MEMFS.
	home := shellEnv.Get("HOME")
	os.MkdirAll(home, 0755)
	os.Chdir(home)

	// Register the LeditWasm global object with all exposed functions.
	ledit := js.ValueOf(map[string]interface{}{
		"init":            js.FuncOf(initFunc),
		"executeCommand":  js.FuncOf(executeCommandFunc),
		"autoComplete":    js.FuncOf(autoCompleteFunc),
		"getCwd":          js.FuncOf(getCwdFunc),
		"changeDir":       js.FuncOf(changeDirFunc),
		"writeFile":       js.FuncOf(writeFileFunc),
		"readFile":        js.FuncOf(readFileFunc),
		"listDir":         js.FuncOf(listDirFunc),
		"deleteFile":      js.FuncOf(deleteFileFunc),
		"getHistory":      js.FuncOf(getHistoryFunc),
		"getEnv":          js.FuncOf(getEnvFunc),
	})

	js.Global().Set("LeditWasm", ledit)

	// fmt.Println("[ledit-wasm] LeditWasm module loaded. Call LeditWasm.init() to initialize.")

	// Block forever so the WASM module stays alive.
	c := make(chan struct{}, 0)
	<-c
}

// ─── JS Bridge Functions ────────────────────────────────────────────────

// initFunc initializes the WASM module. JS must set window.__leditStore
// before calling this. Returns an error string (empty on success).
func initFunc(this js.Value, args []js.Value) interface{} {
	if len(args) > 0 {
		// Optional config object can be passed.
		// config.home string — override home directory
		cfg := args[0]
		if cfg.Type() == js.TypeObject {
			homeKey := cfg.Get("home")
			if homeKey.Type() == js.TypeString {
				h := homeKey.String()
				os.MkdirAll(h, 0755)
				os.Chdir(h)
				shellEnv.Set("HOME", h)
				shellEnv.Set("PWD", h)
			}
		}
	}

	errMsg := store.initStore()
	return errMsg
}

// executeCommandFunc executes a shell command string and returns JSON result.
// Input: command string
// Output: JSON { stdout, stderr, exitCode }
func executeCommandFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return jsonResult(CmdResult{
			Stderr:   "executeCommand: missing argument\n",
			ExitCode: 1,
		})
	}

	input := args[0].String()
	result := parseAndExecute(input)
	return jsonResult(result)
}

// autoCompleteFunc performs tab completion on the input.
// Input: partial command string
// Output: JSON { completions: [...] }
func autoCompleteFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return "{}"
	}
	input := args[0].String()
	return autoCompleteJSON(input)
}

// getCwdFunc returns the current working directory.
func getCwdFunc(this js.Value, args []js.Value) interface{} {
	cwd, err := os.Getwd()
	if err != nil {
		return shellEnv.Get("PWD")
	}
	return cwd
}

// changeDirFunc changes the current directory.
// Input: directory path
// Output: JSON { cwd, error }
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
	if dir == "~" {
		dir = shellEnv.Get("HOME")
	} else if strings.HasPrefix(dir, "~/") {
		dir = shellEnv.Get("HOME") + dir[1:]
	}

	target := resolvePath(dir)
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		r := result{Error: fmt.Sprintf("cd: %s: No such directory", dir)}
		data, _ := json.Marshal(r)
		return string(data)
	}

	if err := os.Chdir(target); err != nil {
		r := result{Error: fmt.Sprintf("cd: %s: %s", dir, err.Error())}
		data, _ := json.Marshal(r)
		return string(data)
	}

	abs, _ := filepath.Abs(target)
	shellEnv.Set("PWD", abs)

	r := result{CWD: abs}
	data, _ := json.Marshal(r)
	return string(data)
}

// writeFileFunc writes content to a file.
// Input: path, content
// Output: error string or ""
func writeFileFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return "writeFile: requires path and content arguments"
	}

	path := args[0].String()
	content := args[1].String()

	if err := SyncWriteFile(resolvePath(path), content); err != nil {
		return err.Error()
	}
	return ""
}

// readFileFunc reads a file's content.
// Input: path
// Output: JSON { content, error }
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
	content, err := ReadFileContent(path)
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
// Input: path
// Output: JSON { entries: [{name, type, size, mode}] }
func listDirFunc(this js.Value, args []js.Value) interface{} {
	type result struct {
		Entries []DirEntry `json:"entries"`
		Error   string     `json:"error"`
	}

	path := "."
	if len(args) > 0 {
		path = args[0].String()
	}

	jsonStr, err := ListDirEntryJSON(path)
	if err != nil {
		r := result{Error: err.Error()}
		data, _ := json.Marshal(r)
		return string(data)
	}

	return jsonStr
}

// deleteFileFunc deletes a file.
// Input: path
// Output: error string or ""
func deleteFileFunc(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return "deleteFile: missing path argument"
	}

	path := args[0].String()
	if err := DeleteFilePath(path); err != nil {
		return err.Error()
	}
	return ""
}

// getHistoryFunc returns the command history as JSON array.
func getHistoryFunc(this js.Value, args []js.Value) interface{} {
	data, _ := json.Marshal(commandHistory)
	return string(data)
}

// getEnvFunc returns all environment variables as JSON object.
func getEnvFunc(this js.Value, args []js.Value) interface{} {
	data, _ := json.Marshal(shellEnv.All())
	return string(data)
}

