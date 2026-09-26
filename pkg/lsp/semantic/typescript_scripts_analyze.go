package semantic

// typeScriptAnalyzerAnalyzeScript — the analysis half of the TypeScript
// analyzer core script (the analyze() entry point and its method dispatch),
// concatenated with typeScriptAnalyzerSessionScript into
// typeScriptAnalyzerCoreScript (typescript_scripts.go). Split out of
// typescript_scripts.go.

const typeScriptAnalyzerAnalyzeScript = `function analyze(input) {
  const workspaceRoot = input.workspaceRoot || process.cwd();
  const filePath = path.resolve(input.filePath || '');
  const fileContent = typeof input.content === 'string' ? input.content : '';
  const method = (input.method || '').toLowerCase();

  const session = getSession(workspaceRoot);
  if (!session) {
    return {
      capabilities: { diagnostics: false, definition: false, hover: false, rename: false, references: false },
      error: 'typescript_not_available'};
  }
  const ts = session.ts;
  const ls = session.ls;

  // Register the current buffer so the persistent LanguageService sees unsaved
  // edits; the next keystroke reuses the same program instead of a fresh parse.
  // Delete-then-set moves the key to the map tail so eviction below drops the
  // least-recently-edited file (Map.set on an existing key does NOT reorder).
  session.pendingContents.delete(filePath);
  session.pendingContents.set(filePath, fileContent);
  // Bound the buffer cache: Map preserves insertion order, so evicting the
  // first key drops the least-recently-edited file. Without a cap, every
  // distinct file edited over the worker's lifetime would accumulate here.
  if (session.pendingContents.size > 64) {
    const oldest = session.pendingContents.keys().next().value;
    session.pendingContents.delete(oldest);
  }
  const resolvedFile = path.resolve(filePath);
  const nextVersion = (parseInt(session.versions.get(resolvedFile) || '0', 10) || 0) + 1;
  session.versions.set(resolvedFile, String(nextVersion));
  if (!session.fileNames.includes(resolvedFile)) {
    session.fileNames.push(resolvedFile);
  }

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
      const sourceText = session.pendingContents.get(filePath) || ts.sys.readFile(filePath) || '';
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

  if (method === 'signature_help') {
    try {
      const pos = input.position || { line: 1, column: 1 };
      const offset = lineColToOffset(fileContent, pos.line, pos.column);
      const helpInfo = ls.getSignatureHelpItems(filePath, offset, {
        triggerReason: { triggerKind: input.trigger === 'edit' ? ts.SignatureHelpTriggerKind.CharacterTyped : ts.SignatureHelpTriggerKind.Invoked }
      });

      if (!helpInfo || helpInfo.items.length === 0) {
        return {
          capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true, code_actions: true, inlay_hints: true, signature_help: true },
          signature_help: { signatures: [], activeSignature: 0, activeParameter: 0 }
        };
      }

      const signatures = [];
      for (const item of helpInfo.items) {
        const parameters = [];
        for (const param of item.parameters) {
          parameters.push({
            label: param.displayParts.map((p) => p.text).join(''),
            documentation: param.documentation ? param.documentation.map((d) => d.text).join('') : ''
          });
        }

        const prefixLabel = item.prefixDisplayParts ? item.prefixDisplayParts.map((p) => p.text).join('') : '';
        const suffixLabel = item.suffixDisplayParts ? item.suffixDisplayParts.map((p) => p.text).join('') : '';
        const sepLabel = item.separatorDisplayParts ? item.separatorDisplayParts.map((p) => p.text).join('') : ', ';
        const paramLabel = item.parameters.map((p) => p.displayParts.map((d) => d.text).join('')).join(sepLabel);
        const label = prefixLabel + paramLabel + suffixLabel;

        let doc = '';
        if (item.documentation) {
          doc = item.documentation.map((d) => d.text).join('');
        }

        signatures.push({
          label: label,
          documentation: doc,
          parameters: parameters
        });
      }

      return {
        capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true, code_actions: true, inlay_hints: true, signature_help: true },
        signature_help: {
          signatures: signatures,
          activeSignature: helpInfo.selectedItemIndex || 0,
          activeParameter: helpInfo.argumentCount > 0 ? helpInfo.argumentCount - 1 : 0
        }
      };
    } catch (err) {
      return {
        capabilities: { diagnostics: true, definition: true, hover: true, rename: true, references: true, code_actions: true, inlay_hints: true, signature_help: false },
        signature_help: { signatures: [], activeSignature: 0, activeParameter: 0 },
        error: String(err && err.message ? err.message : err)
      };
    }
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