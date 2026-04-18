package semantic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type typeScriptAdapter struct{}

// NewTypeScriptAdapter constructs a TS/JS semantic adapter.
func NewTypeScriptAdapter() Adapter {
	return typeScriptAdapter{}
}

func (a typeScriptAdapter) Run(input ToolInput) (ToolResult, error) {
	return runTypeScriptTool(input)
}

func runTypeScriptTool(input ToolInput) (ToolResult, error) {
	var out ToolResult

	in, err := json.Marshal(input)
	if err != nil {
		return out, err
	}

	cmd := exec.Command("node", "-e", typeScriptNodeScript)
	cmd.Stdin = bytes.NewReader(in)
	cmd.Dir = input.WorkspaceRoot

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return out, fmt.Errorf("ts/js semantic tool failed: %s", errMsg)
	}

	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return out, fmt.Errorf("ts/js semantic tool output parse failed: %w", err)
	}

	return out, nil
}

const typeScriptNodeScript = `
const fs = require('node:fs');
const path = require('node:path');

function readStdin() {
  return new Promise((resolve, reject) => {
    let data = '';
    process.stdin.setEncoding('utf8');
    process.stdin.on('data', (chunk) => (data += chunk));
    process.stdin.on('end', () => resolve(data));
    process.stdin.on('error', reject);
  });
}

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

(async () => {
  try {
    const raw = await readStdin();
    const input = JSON.parse(raw || '{}');
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
      process.stdout.write(JSON.stringify({
        capabilities: { diagnostics: false, definition: false },
        error: 'typescript_not_available'
      }));
      return;
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
        process.stdout.write(JSON.stringify({
          capabilities: { diagnostics: true, definition: true },
          definition: null
        }));
        return;
      }
      const targetPath = path.resolve(first.fileName);
      let targetText = '';
      if (targetPath === filePath) targetText = fileContent;
      else if (fs.existsSync(targetPath)) targetText = fs.readFileSync(targetPath, 'utf8');

      const source = ts.createSourceFile(targetPath, targetText, ts.ScriptTarget.Latest, true);
      const lc = source.getLineAndCharacterOfPosition(first.textSpan.start);
      process.stdout.write(JSON.stringify({
        capabilities: { diagnostics: true, definition: true },
        definition: {
          path: targetPath,
          line: lc.line + 1,
          column: lc.character + 1,
        }
      }));
      return;
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

    process.stdout.write(JSON.stringify({
      capabilities: { diagnostics: true, definition: true },
      diagnostics
    }));
  } catch (err) {
    process.stdout.write(JSON.stringify({
      capabilities: { diagnostics: false, definition: false },
      error: String(err && err.message ? err.message : err)
    }));
  }
})();
`
