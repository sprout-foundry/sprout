package semantic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type typeScriptSessionAdapter struct {
	mu     sync.Mutex
	closed bool
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr bytes.Buffer
}

// NewTypeScriptSessionPool creates a reusable per-workspace adapter pool for
// TypeScript-family languages backed by a persistent Node worker process.
func NewTypeScriptSessionPool(idleTTL time.Duration) *SessionPool {
	return NewSessionPool(func(workspaceRoot string) (SessionAdapter, error) {
		_ = workspaceRoot
		return &typeScriptSessionAdapter{}, nil
	}, idleTTL)
}

func (a *typeScriptSessionAdapter) Run(input ToolInput) (ToolResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return ToolResult{}, fmt.Errorf("typescript session closed")
	}

	if err := a.ensureWorkerLocked(input.WorkspaceRoot); err != nil {
		return ToolResult{}, err
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return ToolResult{}, err
	}

	if _, err := a.stdin.Write(append(payload, '\n')); err != nil {
		a.resetWorkerLocked()
		return ToolResult{}, fmt.Errorf("typescript worker write failed: %w", err)
	}

	line, err := a.stdout.ReadBytes('\n')
	if err != nil {
		errMsg := strings.TrimSpace(a.stderr.String())
		a.resetWorkerLocked()
		if errMsg == "" {
			return ToolResult{}, fmt.Errorf("typescript worker read failed: %w", err)
		}
		return ToolResult{}, fmt.Errorf("typescript worker read failed: %w (%s)", err, errMsg)
	}

	var result ToolResult
	if err := json.Unmarshal(bytes.TrimSpace(line), &result); err != nil {
		return ToolResult{}, fmt.Errorf("typescript worker response parse failed: %w", err)
	}
	return result, nil
}

func (a *typeScriptSessionAdapter) Healthy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	if a.cmd == nil || a.cmd.Process == nil {
		return false
	}
	return a.cmd.ProcessState == nil
}

func (a *typeScriptSessionAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	a.resetWorkerLocked()
	return nil
}

func (a *typeScriptSessionAdapter) ensureWorkerLocked(workspaceRoot string) error {
	if a.cmd != nil && a.cmd.Process != nil && a.cmd.ProcessState == nil {
		return nil
	}

	cmd := exec.Command("node", "-e", typeScriptNodeWorkerScript)
	cmd.Dir = workspaceRoot

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("typescript worker stdin pipe failed: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("typescript worker stdout pipe failed: %w", err)
	}
	a.stderr.Reset()
	cmd.Stderr = &a.stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("typescript worker start failed: %w", err)
	}

	a.cmd = cmd
	a.stdin = stdin
	a.stdout = bufio.NewReader(stdout)
	return nil
}

func (a *typeScriptSessionAdapter) resetWorkerLocked() {
	if a.stdin != nil {
		_ = a.stdin.Close()
		a.stdin = nil
	}
	if a.cmd != nil {
		if a.cmd.Process != nil && a.cmd.ProcessState == nil {
			_ = a.cmd.Process.Kill()
		}
		_ = a.cmd.Wait()
		a.cmd = nil
	}
	a.stdout = nil
}

const typeScriptNodeWorkerScript = `
const fs = require('node:fs');
const path = require('node:path');
const readline = require('node:readline');

function normalizeSeverity(cat, ts) {
  if (cat === ts.DiagnosticCategory.Error) return 'error';
  if (cat === ts.DiagnosticCategory.Warning) return 'warning';
  return 'info';
}

function buildLineStarts(text) {
  const starts = [0];
  for (let i = 0; i < text.length; i++) {
    if (text.charCodeAt(i) === 10) starts.push(i + 1);
  }
  return starts;
}

function lineColToOffset(text, line, column) {
  const starts = buildLineStarts(text);
  const lineIdx = Math.max(0, Math.min((line || 1) - 1, starts.length - 1));
  const lineStart = starts[lineIdx];
  const nextLineStart = lineIdx + 1 < starts.length ? starts[lineIdx + 1] : text.length + 1;
  const lineLen = Math.max(0, nextLineStart - lineStart - 1);
  const col = Math.max(1, Math.min(column || 1, lineLen + 1));
  return lineStart + (col - 1);
}

function analyze(input) {
  const workspaceRoot = input.workspaceRoot || process.cwd();
  const filePath = path.resolve(input.filePath || '');
  const fileContent = typeof input.content === 'string' ? input.content : '';
  const method = (input.method || '').toLowerCase();

  let ts;
  try {
    const candidates = [
      path.join(workspaceRoot, 'node_modules'),
      path.join(workspaceRoot, 'webui', 'node_modules'),
    ];
    for (const base of candidates) {
      try {
        const resolved = require.resolve('typescript', { paths: [base] });
        ts = require(resolved);
        break;
      } catch (_) {
        // Try next candidate path.
      }
    }
    if (!ts) {
      ts = require('typescript');
    }
  } catch (_) {
    return {
      capabilities: { diagnostics: false, definition: false },
      error: 'typescript_not_available'
    };
  }

  let compilerOptions = {
    target: ts.ScriptTarget.ESNext,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.NodeJs,
    jsx: ts.JsxEmit.ReactJSX,
    allowJs: true,
    checkJs: true,
    skipLibCheck: true,
    esModuleInterop: true,
    allowSyntheticDefaultImports: true,
    resolveJsonModule: true,
    types: []
  };

  let fileNames = [filePath];
  try {
    const cfgPath = ts.findConfigFile(workspaceRoot, ts.sys.fileExists, 'tsconfig.json');
    if (cfgPath) {
      const configText = ts.sys.readFile(cfgPath);
      if (configText) {
        const parsedCfg = ts.parseConfigFileTextToJson(cfgPath, configText);
        if (!parsedCfg.error) {
          const parsed = ts.parseJsonConfigFileContent(parsedCfg.config, ts.sys, path.dirname(cfgPath));
          if (parsed && parsed.options) compilerOptions = { ...compilerOptions, ...parsed.options };
          if (parsed && Array.isArray(parsed.fileNames) && parsed.fileNames.length > 0) fileNames = parsed.fileNames;
        }
      }
    }
  } catch (_) {
    // Best effort: keep defaults.
  }

  if (!fileNames.includes(filePath)) fileNames.push(filePath);

  const versions = new Map();
  for (const f of fileNames) versions.set(f, '1');

  const host = {
    getScriptFileNames: () => fileNames,
    getScriptVersion: (f) => versions.get(f) || '1',
    getScriptSnapshot: (f) => {
      if (path.resolve(f) === filePath) {
        return ts.ScriptSnapshot.fromString(fileContent);
      }
      if (!fs.existsSync(f)) return undefined;
      return ts.ScriptSnapshot.fromString(fs.readFileSync(f, 'utf8'));
    },
    getCurrentDirectory: () => workspaceRoot,
    getCompilationSettings: () => compilerOptions,
    getDefaultLibFileName: (options) => ts.getDefaultLibFilePath(options),
    fileExists: ts.sys.fileExists,
    readFile: ts.sys.readFile,
    readDirectory: ts.sys.readDirectory,
    directoryExists: ts.sys.directoryExists,
    getDirectories: ts.sys.getDirectories,
    useCaseSensitiveFileNames: () => ts.sys.useCaseSensitiveFileNames,
    getNewLine: () => ts.sys.newLine,
  };

  const ls = ts.createLanguageService(host, ts.createDocumentRegistry());

  if (method === 'definition') {
    const pos = input.position || { line: 1, column: 1 };
    const offset = lineColToOffset(fileContent, pos.line, pos.column);
    const defs = ls.getDefinitionAtPosition(filePath, offset) || [];
    const first = defs[0];
    if (!first) {
      return {
        capabilities: { diagnostics: true, definition: true },
        definition: null
      };
    }
    const targetPath = path.resolve(first.fileName);
    let targetText = '';
    if (targetPath === filePath) targetText = fileContent;
    else if (fs.existsSync(targetPath)) targetText = fs.readFileSync(targetPath, 'utf8');

    const source = ts.createSourceFile(targetPath, targetText, ts.ScriptTarget.Latest, true);
    const lc = source.getLineAndCharacterOfPosition(first.textSpan.start);
    return {
      capabilities: { diagnostics: true, definition: true },
      definition: {
        path: targetPath,
        line: lc.line + 1,
        column: lc.character + 1,
      }
    };
  }

  const syntactic = ls.getSyntacticDiagnostics(filePath) || [];
  const semantic = ls.getSemanticDiagnostics(filePath) || [];
  const all = syntactic.concat(semantic);
  const diagnostics = all.map((d) => {
    const start = typeof d.start === 'number' ? d.start : 0;
    const len = typeof d.length === 'number' ? d.length : 1;
    const msg = ts.flattenDiagnosticMessageText(d.messageText, '\n');
    return {
      from: start,
      to: Math.max(start + len, start + 1),
      severity: normalizeSeverity(d.category, ts),
      message: msg,
      source: 'typescript'
    };
  });

  return {
    capabilities: { diagnostics: true, definition: true },
    diagnostics
  };
}

const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
rl.on('line', (line) => {
  let input = {};
  try {
    input = JSON.parse(line || '{}');
  } catch (err) {
    const out = {
      capabilities: { diagnostics: false, definition: false },
      error: String(err && err.message ? err.message : err),
    };
    process.stdout.write(JSON.stringify(out) + '\\n');
    return;
  }

  try {
    const out = analyze(input);
    process.stdout.write(JSON.stringify(out) + '\\n');
  } catch (err) {
    const out = {
      capabilities: { diagnostics: false, definition: false },
      error: String(err && err.message ? err.message : err),
    };
    process.stdout.write(JSON.stringify(out) + '\\n');
  }
});
`
