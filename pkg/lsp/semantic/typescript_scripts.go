package semantic

// Shared JavaScript used by both the one-shot and persistent TypeScript adapters.
const typeScriptAnalyzerCoreScript = `
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
      capabilities: { diagnostics: false, definition: false, hover: false, rename: false, references: false },
      error: 'typescript_not_available'};
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
        capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true },
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
      capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true },
      definition: {
        path: targetPath,
        line: lc.line + 1,
        column: lc.character + 1,
      }
    };
  }

  if (method === 'hover') {
    const pos = input.position || { line: 1, column: 1 };
    const offset = lineColToOffset(fileContent, pos.line, pos.column);
    const info = ls.getQuickInfoAtPosition(filePath, offset);
    if (!info) {
      return {
        capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true },
        hover: null
      };
    }
    const displayParts = info.displayParts || [];
    const docs = info.documentation || [];
    let contents = displayParts.map((p) => p.text).join('');
    if (docs.length > 0) {
      const docText = docs.map((p) => p.text).join('\n');
      contents = contents + '\n\n' + docText;
    }
    return {
      capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true },
      hover: { contents: contents }
    };
  }

  if (method === 'rename') {
    const pos = input.position || { line: 1, column: 1 };
    const offset = lineColToOffset(fileContent, pos.line, pos.column);
    const renameInfo = ls.getRenameInfoAtPosition(filePath, offset);
    if (!renameInfo || !renameInfo.canRename) {
      return {
        capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true },
        rename: { locations: [] }
      };
    }
    // Find all references in the current file to get locations
    const refs = ls.findReferences(filePath, offset) || [];
    const currentFileRefs = refs.filter(r => r.fileName === filePath);
    const locations = [];
    const seen = new Set();
    for (const ref of currentFileRefs) {
      if (!ref.textSpan) continue;
      const key = ref.textSpan.start + ':' + ref.textSpan.length;
      if (seen.has(key)) continue;
      seen.add(key);
      locations.push({
        filePath: filePath,
        from: ref.textSpan.start,
        to: ref.textSpan.start + ref.textSpan.length,
      });
    }
    locations.sort((a, b) => a.from - b.from);
    return {
      capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true },
      rename: { locations }
    };
  }

  if (method === 'references') {
    const pos = input.position || { line: 1, column: 1 };
    const offset = lineColToOffset(fileContent, pos.line, pos.column);
    const renameInfo = ls.getRenameInfoAtPosition(filePath, offset);
    if (!renameInfo || !renameInfo.canRename) {
      return {
        capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true },
        references: { locations: [], symbolName: '' }
      };
    }
    const symbolName = renameInfo.displayName || '<symbol>';
    const refs = ls.findReferences(filePath, offset) || [];
    const locations = [];
    const seen = new Set();
    for (const ref of refs) {
      if (!ref.textSpan) continue;
      const refPath = ref.fileName;
      // Read line text for the reference
      let lineText = '';
      try {
        if (refPath === filePath) {
          lineText = fileContent;
        } else if (fs.existsSync(refPath)) {
          lineText = fs.readFileSync(refPath, 'utf8');
        }
      } catch (e) {
        console.error('Failed to read reference file:', refPath, e);
      }

      // Get line number and column from the text span
      const lineStarts = buildLineStarts(lineText);
      let lineNum = 1;
      let startCol = 1;
      for (let i = 0; i < lineStarts.length - 1 && lineStarts[i + 1] <= ref.textSpan.start; i++) {
        lineNum = i + 2;
      }
      startCol = ref.textSpan.start - lineStarts[Math.max(0, lineNum - 1)] + 1;
      const endCol = startCol + ref.textSpan.length - 1;

      // Extract the actual line text
      const lines = lineText.split('\n');
      const actualLineText = lines[lineNum - 1] || '';

      const key = refPath + ':' + ref.textSpan.start;
      if (seen.has(key)) continue;
      seen.add(key);
      locations.push({
        filePath: refPath,
        line: lineNum,
        startCol: startCol,
        endCol: endCol,
        lineText: actualLineText
      });
    }

    // Sort: current file first, then by path, then by line
    locations.sort((a, b) => {
      if (a.filePath === b.filePath) return a.line - b.line;
      if (a.filePath === filePath) return -1;
      if (b.filePath === filePath) return 1;
      return a.filePath.localeCompare(b.filePath);
    });

    return {
      capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true },
      references: { locations, symbolName }
    };
  }

  if (method === 'code_actions') {
    const pos = input.position || { line: 1, column: 1 };
    const offset = lineColToOffset(fileContent, pos.line, pos.column);
    const actions = [];

    // Organize imports (add missing + remove unused)
    try {
      const changes = ls.organizeImports({ fileName: filePath, type: "file", mode: ts.OrganizeImportsMode.All });
      if (changes && changes.length > 0) {
        const edits = [];
        for (const change of changes) {
          for (const tc of change.textChanges) {
            edits.push({
              filePath: filePath,
              from: tc.start,
              to: tc.start + tc.length,
              newText: tc.newText
            });
          }
        }
        if (edits.length > 0) {
          actions.push({ title: 'Organize Imports', kind: 'source.organizeImports', edits });
        }
      }
    } catch (_) {}

    // Get code fixes at position (for individual diagnostics like missing imports)
    try {
      const syntactic = ls.getSyntacticDiagnostics(filePath) || [];
      const semantic = ls.getSemanticDiagnostics(filePath) || [];
      const allDiagnostics = syntactic.concat(semantic);

      // Filter diagnostics at/near the cursor position
      const relevantDiags = allDiagnostics.filter(d => {
        const start = d.start || 0;
        const len = d.length || 0;
        const end = start + len;
        return start <= offset && end >= offset;
      });

      // Collect unique actions from all applicable code fixes
      const seenActions = new Set();
      for (const diag of relevantDiags.slice(0, 5)) { // limit to prevent slowness
        try {
          const fixStart = diag.start || offset;
          const fixEnd = fixStart + (diag.length || 1);
          const fixes = ls.getCodeFixesAtPosition(filePath, fixStart, fixEnd);
          for (const fix of fixes || []) {
            if (seenActions.has(fix.fixName)) continue;
            seenActions.add(fix.fixName);
            const edits = [];
            for (const change of fix.changes || []) {
              for (const tc of change.textChanges || []) {
                edits.push({
                  filePath: change.fileName,
                  from: tc.start,
                  to: tc.start + tc.length,
                  newText: tc.newText
                });
              }
            }
            if (edits.length > 0) {
              // Convert fixName (e.g., "addMissingImport") to title (e.g., "Add Missing Import")
              const title = fix.fixName.replace(/([A-Z])/g, ' $1').replace(/^./, s => s.toUpperCase()).trim();
              actions.push({
                title: title,
                kind: 'quickfix',
                edits
              });
            }
          }
        } catch (_) {}
      }
    } catch (_) {}

    // Add all quick fixes across the file
    try {
      const combinedFix = ls.getCombinedCodeFix({ fileName: filePath }, "fixAll", {});
      if (combinedFix && combinedFix.changes && combinedFix.changes.length > 0) {
        const edits = [];
        for (const change of combinedFix.changes) {
          for (const tc of change.textChanges || []) {
            edits.push({
              filePath: change.fileName,
              from: tc.start,
              to: tc.start + tc.length,
              newText: tc.newText
            });
          }
        }
        if (edits.length > 0) {
          actions.push({ title: 'Fix All', kind: 'quickfix', edits });
        }
      }
    } catch (_) {}

    return {
      capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true, code_actions: true },
      code_actions: actions
    };
  }

  if (method === 'inlay_hints') {
    const hints = [];
    try {
      const sourceText = ts.sys.readFile(filePath) || '';
      const inlayHints = ls.provideInlayHints(filePath, { start: 0, length: sourceText.length }, {
        includeInlayHints: true,
        includeInlayParameterNameHints: 'all',
        includeInlayParameterNameHintsWhenArgumentMatchesName: true,
        includeInlayVariableTypeHints: true,
        includeInlayFunctionCallArguments: true,
        includeInlayEnumMemberValueHints: true,
        includeInlayFunctionLikeSignatureHint: true,
        includeInlayPropertyNameHints: true,
      }) || [];
      for (const hint of inlayHints) {
        const label = hint.text || '';
        let kind = 'none';
        if (hint.kind === 'Type') kind = 'type';
        else if (hint.kind === 'Parameter') kind = 'parameter';
        else if (hint.kind === 'Enum') kind = 'type';
        hints.push({
          from: hint.position,
          to: hint.position,
          label: label,
          kind: kind,
        });
      }
    } catch (err) {
      return {
        capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true, code_actions: true, inlay_hints: false },
        inlay_hints: [],
        error: String(err && err.message ? err.message : err)
      };
    }

    return {
      capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true, code_actions: true, inlay_hints: true },
      inlay_hints: hints
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
    capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true },
    diagnostics
  };
}
`

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
      capabilities: { diagnostics: false, definition: false, hover: false, rename: false },
      error: String(err && err.message ? err.message : err),
    };
    process.stdout.write(JSON.stringify(out) + '\n');
  }
});
`
