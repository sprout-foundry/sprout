package semantic

// Shared JavaScript used by both the one-shot and persistent TypeScript
// adapters.
//
// The core analyzer script is split into two concatenated parts
// (session-management + analysis engine) so no single file exceeds the
// 500-line limit; the concatenation is byte-identical to the original
// literal (pinned by the hash in TestTypeScriptAnalyzerCoreScriptStable).

// typeScriptAnalyzerSessionScript holds the session-management half of the
// TypeScript analyzer core script: severity normalization, line-offset
// helpers, and the per-workspace ts.LanguageService cache.
const typeScriptAnalyzerSessionScript = `
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

// The worker process is long-lived and analyze runs per keystroke, so cache one
// LanguageService per workspace and only update the file that changed; a full
// program rebuild per request is what made editing TypeScript files laggy.
const workspaceCache = new Map();

function sessionConfigChanged(s) {
  try {
    if (s.configPath) {
      return fs.statSync(s.configPath).mtimeMs !== s.configMtimeMs;
    }
    return !!s.ts.findConfigFile(s.workspaceRoot, s.ts.sys.fileExists, 'tsconfig.json');
  } catch (_) {
    return true;
  }
}

function createSession(workspaceRoot) {
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
    return null;
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

  let fileNames = [];
  let configPath = null;
  let configMtimeMs = null;
  try {
    const cfgPath = ts.findConfigFile(workspaceRoot, ts.sys.fileExists, 'tsconfig.json');
    if (cfgPath) {
      configPath = cfgPath;
      configMtimeMs = fs.statSync(cfgPath).mtimeMs;
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

  const versions = new Map();
  for (const f of fileNames) versions.set(f, '1');

  // Keyed by resolved path so snapshot/version lookups match the path form TS
  // passes back to the host for files added after session creation.
  const pendingContents = new Map();

  const host = {
    getScriptFileNames: () => fileNames,
    getScriptVersion: (f) => {
      const resolved = path.resolve(f);
      // Files with unsaved buffer content are versioned by the per-session
      // counter. Everything else derives its version from disk mtime so the
      // LanguageService re-reads a file the moment it changes on disk —
      // otherwise imported files freeze at version '1' and cross-file
      // diagnostics go stale until the session is rebuilt.
      if (pendingContents.has(resolved)) return versions.get(resolved) || '1';
      try { return String(fs.statSync(resolved).mtimeMs); } catch (_) { return '0'; }
    },
    getScriptSnapshot: (f) => {
      const resolved = path.resolve(f);
      if (pendingContents.has(resolved)) {
        return ts.ScriptSnapshot.fromString(pendingContents.get(resolved));
      }
      if (!fs.existsSync(resolved)) return undefined;
      return ts.ScriptSnapshot.fromString(fs.readFileSync(resolved, 'utf8'));
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

  return {
    workspaceRoot,
    ts,
    ls,
    fileNames,
    compilerOptions,
    configPath,
    configMtimeMs,
    pendingContents,
    versions,
  };
}

function getSession(workspaceRoot) {
  const cached = workspaceCache.get(workspaceRoot);
  if (cached && !sessionConfigChanged(cached)) {
    return cached;
  }
  const session = createSession(workspaceRoot);
  if (session) {
    workspaceCache.set(workspaceRoot, session);
  }
  return session;
}

`

const typeScriptAnalyzerCoreScript = typeScriptAnalyzerSessionScript + typeScriptAnalyzerAnalyzeScript

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
` + typeScriptAnalyzerCoreScript + `

(async () => {
  try {
    const raw = await readStdin();
    const input = JSON.parse(raw || '{}');
    const out = analyze(input);
    process.stdout.write(JSON.stringify(out));
  } catch (err) {
    process.stdout.write(JSON.stringify({
      capabilities: { diagnostics: false, definition: false, hover: false, rename: false },
      error: String(err && err.message ? err.message : err)
    }));
  }
})();
`

const typeScriptNodeWorkerScript = `
const fs = require('node:fs');
const path = require('node:path');
const readline = require('node:readline');
` + typeScriptAnalyzerCoreScript + `

const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
rl.on('line', (line) => {
  let input = {};
  try {
    input = JSON.parse(line || '{}');
  } catch (err) {
    const out = {
      capabilities: { diagnostics: false, definition: false, hover: false, rename: false },
      error: String(err && err.message ? err.message : err),
    };
    process.stdout.write(JSON.stringify(out) + '\n');
    return;
  }

  try {
    const out = analyze(input);
    process.stdout.write(JSON.stringify(out) + '\n');
  } catch (err) {
    const out = {
      capabilities: { diagnostics: false, definition: false, hover: false, rename: false, signature_help: false },
      error: String(err && err.message ? err.message : err),
    };
    process.stdout.write(JSON.stringify(out) + '\n');
  }
});
`
