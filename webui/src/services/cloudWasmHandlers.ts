/**
 * WASM-local endpoint handlers for CloudAdapter.
 *
 * In cloud mode, file operations are handled client-side by the WASM shell
 * rather than being proxied to a backend. This module contains all the
 * request handlers for wasm-local endpoints and their supporting utilities.
 */

import type { WasmShell } from './wasmShell';

/**
 * Shell-escape an argument for use in a command string.
 * Wraps in single quotes and escapes any embedded single quotes.
 */
function shellEscapeArg(arg: string): string {
  return `'${arg.replace(/'/g, "'\\''")}'`;
}

/**
 * Handle wasm-local endpoints by routing them to the WASM shell.
 * These endpoints would normally go to the Go backend, but in cloud mode
 * the WASM shell owns the virtual filesystem.
 */
export function handleWasmLocal(
  shell: WasmShell,
  urlPath: string,
  method: string,
  fullUrl: string,
  bodyStr?: string,
): Response {
  try {
    switch (urlPath) {
      // ── File listing ──────────────────────────────────────────
      case '/api/files':
        return handleWasmFileList(shell, fullUrl);
      case '/api/browse':
      case '/api/workspace/browse':
        return handleWasmBrowse(shell, fullUrl);

      // ── File read/write ──────────────────────────────────────
      case '/api/file':
        return handleWasmFile(shell, method, fullUrl, bodyStr);

      // ── File CRUD ────────────────────────────────────────────
      case '/api/create':
        return handleWasmCreate(shell, bodyStr);
      case '/api/delete':
        return handleWasmDelete(shell, bodyStr);
      case '/api/rename':
        return handleWasmRename(shell, bodyStr);

      // ── Search ──────────────────────────────────────────────
      case '/api/search':
        return handleWasmSearch(shell, fullUrl);
      case '/api/search/replace':
        return handleWasmSearchReplace(shell, bodyStr);

      // ── File metadata ──────────────────────────────────────
      case '/api/file/check-modified':
        return handleWasmCheckModified(shell, bodyStr);
      case '/api/file/consent':
        // No consent flow needed in cloud/WASM mode
        return jsonOk({ token: 'wasm-local', path: '/', operation: 'read', expires_at: '' });
      case '/api/files/prettier-config':
        return jsonOk({ prettier: null });

      // ── Terminal stubs ──────────────────────────────────────
      case '/api/terminal/sessions':
        return jsonOk({ active_count: 0, count: 0 });
      case '/api/terminal/shells':
        return jsonOk({ shells: [{ name: 'wasm', path: '/bin/wasm', default: true }] });
      case '/api/terminal/history':
        if (method === 'POST') {
          // Accept but ignore — WASM terminal manages its own history
          return jsonOk({ success: true });
        }
        return jsonOk({ entries: [] });

      default:
        // Unrecognized wasm-local endpoint — return empty OK
        console.warn('[CloudAdapter] Unhandled wasm-local endpoint:', urlPath);
        return jsonOk({});
    }
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    console.error(`[CloudAdapter] WASM handler error for ${urlPath}:`, err);
    return jsonError(message, 500);
  }
}

// ── Individual wasm-local route handlers ─────────────────────────

/**
 * GET /api/files — Returns all files in the workspace.
 * Supports optional ?path= query parameter for browsing subdirectories.
 * The webui expects { message: string, files: Array<{path, modified}> }
 */
function handleWasmFileList(shell: WasmShell, fullUrl?: string): Response {
  const cwd = fullUrl ? getQueryParam(fullUrl, 'path') || shell.getCwd() : shell.getCwd();
  const result = shell.listDir(cwd);
  if (result.error) {
    return jsonError(result.error, 500);
  }
  // Build a flat recursive file list from the WASM directory tree.
  // The webui getFiles() expects { message, files: [{path, modified}] }
  const files = flattenEntries(shell, cwd);
  return jsonOk({ message: 'success', files });
}

/**
 * Recursively flatten WASM directory entries into a flat file list.
 * Each entry includes name (extracted from path) for the FileTree component.
 */
function flattenEntries(shell: WasmShell, dir: string): Array<{ path: string; modified: boolean; name: string }> {
  const result: Array<{ path: string; modified: boolean; name: string }> = [];
  const listResult = shell.listDir(dir);
  if (listResult.error) return result;

  for (const entry of listResult.entries) {
    const fullPath = dir === '/' ? `/${entry.name}` : `${dir}/${entry.name}`;
    if (entry.type === 'dir') {
      result.push(...flattenEntries(shell, fullPath));
    } else {
      result.push({ path: fullPath, modified: false, name: entry.name });
    }
  }
  return result;
}

/**
 * GET /api/browse?path=... — Browse a directory.
 * Expected: { files: [{name, path, type, size, modified}] }
 */
function handleWasmBrowse(shell: WasmShell, fullUrl: string): Response {
  const path = getQueryParam(fullUrl, 'path') || '/';
  const safePath = sanitizePath(path);
  const result = shell.listDir(safePath);
  if (result.error) {
    return jsonError(result.error, 500);
  }
  const files = result.entries.map((entry) => ({
    name: entry.name,
    path: safePath === '/' ? `/${entry.name}` : `${safePath}/${entry.name}`,
    type: entry.type === 'dir' ? 'directory' : 'file',
    size: entry.size,
    modified: 0,
  }));
  return jsonOk({ files });
}

/**
 * GET /api/file?path=...  — Read file content.
 * POST /api/file?path=... — Write file content.
 */
function handleWasmFile(shell: WasmShell, method: string, fullUrl: string, bodyStr?: string): Response {
  const path = getQueryParam(fullUrl, 'path');
  if (!path) {
    return jsonError('Missing path parameter', 400);
  }
  const safePath = sanitizePath(path);

  if (method === 'GET') {
    const result = shell.readFile(safePath);
    if (result.error) {
      return jsonError(result.error, 404);
    }
    // Return raw content with text content-type
    return new Response(result.content, {
      status: 200,
      headers: { 'Content-Type': 'text/plain; charset=utf-8' },
    });
  }

  // POST — write
  if (!bodyStr) {
    return jsonError('Missing request body', 400);
  }
  let content: string;
  try {
    const parsed = JSON.parse(bodyStr);
    content = typeof parsed.content === 'string' ? parsed.content : bodyStr;
  } catch {
    content = bodyStr;
  }
  const err = shell.writeFile(safePath, content);
  if (err) {
    return jsonError(err, 500);
  }
  return jsonOk({ message: 'ok' });
}

/**
 * POST /api/create — Create a file or directory.
 * Body: { path, directory?: boolean } or { directory, path }
 */
function handleWasmCreate(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  const parsed = safeParseJson(bodyStr);
  const path = parsed?.path as string | undefined;
  const isDir = !!(parsed?.directory || parsed?.is_directory);
  if (!path) return jsonError('Missing path', 400);

  const safePath = sanitizePath(path);

  if (isDir) {
    const result = shell.executeCommand(`mkdir -p ${shellEscapeArg(safePath)}`);
    if (result.exitCode !== 0) {
      return jsonError(result.stderr || 'mkdir failed', 500);
    }
  } else {
    // Create an empty file
    const err = shell.writeFile(safePath, '');
    if (err) {
      return jsonError(err, 500);
    }
  }
  return jsonOk({ message: 'ok', path: safePath });
}

/**
 * DELETE /api/delete — Delete a file.
 * Body: { path }
 */
function handleWasmDelete(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  const parsed = safeParseJson(bodyStr);
  const path = parsed?.path as string | undefined;
  if (!path) return jsonError('Missing path', 400);

  const safePath = sanitizePath(path);
  const err = shell.deleteFile(safePath);
  if (err) {
    // If file delete fails, try rm via command (handles directories)
    const result = shell.executeCommand(`rm -rf ${shellEscapeArg(safePath)}`);
    if (result.exitCode !== 0) {
      return jsonError(result.stderr || err, 500);
    }
  }
  return jsonOk({ message: 'ok', path: safePath });
}

/**
 * POST /api/rename — Rename a file or directory.
 * Body: { old_path, new_path }
 */
function handleWasmRename(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  const parsed = safeParseJson(bodyStr);
  const oldPath = parsed?.old_path as string | undefined;
  const newPath = parsed?.new_path as string | undefined;
  if (!oldPath || !newPath) return jsonError('Missing old_path or new_path', 400);

  const safeOld = sanitizePath(oldPath);
  const safeNew = sanitizePath(newPath);
  const result = shell.executeCommand(`mv ${shellEscapeArg(safeOld)} ${shellEscapeArg(safeNew)}`);
  if (result.exitCode !== 0) {
    return jsonError(result.stderr || 'rename failed', 500);
  }
  return jsonOk({ message: 'ok', old_path: safeOld, new_path: safeNew });
}

/**
 * GET /api/search?query=...&case_sensitive=...&regex=...&include=...
 * Uses WASM shell grep command.
 */
function handleWasmSearch(shell: WasmShell, fullUrl: string): Response {
  const query = getQueryParam(fullUrl, 'query') || '';
  const caseSensitive = getQueryParam(fullUrl, 'case_sensitive') === 'true';
  const regex = getQueryParam(fullUrl, 'regex') === 'true';
  const include = getQueryParam(fullUrl, 'include') || '';

  if (!query) return jsonOk({ results: [], total_matches: 0, total_files: 0, truncated: false, query: '' });

  const cwd = shell.getCwd();
  // Build grep command
  let grepCmd = 'grep';
  if (!caseSensitive) grepCmd += ' -i';
  if (regex) grepCmd += ' -E';
  grepCmd += ` -rn --include=${shellEscapeArg(include || '*')} ${shellEscapeArg(query)} ${shellEscapeArg(cwd)}`;

  const result = shell.executeCommand(grepCmd);
  // grep returns exit code 1 when no matches — that's not an error
  if (result.exitCode !== 0 && result.exitCode !== 1) {
    return jsonError(result.stderr || 'search failed', 500);
  }

  // Parse grep output into structured results
  const results = parseGrepOutput(result.stdout);
  const totalMatches = results.reduce((sum, r) => sum + r.match_count, 0);
  return jsonOk({
    results,
    total_matches: totalMatches,
    total_files: results.length,
    truncated: false,
    query,
  });
}

/**
 * POST /api/search/replace — Search and replace across files.
 * Body: { search, replace, files, case_sensitive?, whole_word?, regex?, preview }
 */
function handleWasmSearchReplace(shell: WasmShell, bodyStr?: string): Response {
  if (!bodyStr) return jsonError('Missing request body', 400);
  const parsed = safeParseJson(bodyStr);
  const search = parsed?.search as string | undefined;
  const replace = parsed?.replace as string | undefined;
  const files = parsed?.files as string[] | undefined;
  const preview = !!parsed?.preview;

  if (!search || !replace || !files?.length) {
    return jsonError('Missing search, replace, or files', 400);
  }

  const changes: Array<{
    file: string;
    matches: Array<{
      line_number: number;
      old_line: string;
      new_line: string;
      column_start: number;
      column_end: number;
    }>;
    changed_lines: number;
  }> = [];

  for (const filePath of files) {
    const safePath = sanitizePath(filePath);
    const readResult = shell.readFile(safePath);
    if (readResult.error) continue;

    const content = readResult.content;
    const lines = content.split('\n');
    const matches: Array<{
      line_number: number;
      old_line: string;
      new_line: string;
      column_start: number;
      column_end: number;
    }> = [];
    let changedLines = 0;

    for (let i = 0; i < lines.length; i++) {
      const idx = lines[i].indexOf(search);
      if (idx !== -1) {
        const oldLine = lines[i];
        const newLine = lines[i].replace(search, replace);
        if (oldLine !== newLine) {
          matches.push({
            line_number: i + 1,
            old_line: oldLine,
            new_line: newLine,
            column_start: idx,
            column_end: idx + search.length,
          });
          if (!preview) {
            lines[i] = newLine;
          }
          changedLines++;
        }
      }
    }

    if (matches.length > 0) {
      if (!preview) {
        const err = shell.writeFile(safePath, lines.join('\n'));
        if (err) {
          console.warn(`[CloudAdapter] searchReplace: failed to write ${safePath}:`, err);
        }
      }
      changes.push({ file: safePath, matches, changed_lines: changedLines });
    }
  }

  const totalChanges = changes.reduce((sum, c) => sum + c.changed_lines, 0);
  return jsonOk({ changes, total_changes: totalChanges, preview });
}

/**
 * POST /api/file/check-modified — Check if files have been modified.
 * In WASM mode, files are only modified through the WASM shell, so always
 * return empty (no external modifications detected).
 */
function handleWasmCheckModified(_shell: WasmShell, bodyStr?: string): Response {
  // Parse the request to acknowledge it, but always return no modifications
  // since WASM files can only change through the shell itself.
  void bodyStr;
  return jsonOk({ modified: [] });
}

// ── WASM helper utilities ────────────────────────────────────────

/**
 * Parse grep -rn output into structured search results.
 * Format: "filename:linenum:matched line text"
 */
function parseGrepOutput(output: string): Array<{
  file: string;
  matches: Array<{
    line_number: number;
    line: string;
    column_start: number;
    column_end: number;
    context_before: string[];
    context_after: string[];
  }>;
  match_count: number;
}> {
  const fileMap = new Map<
    string,
    Array<{
      line_number: number;
      line: string;
      column_start: number;
      column_end: number;
      context_before: string[];
      context_after: string[];
    }>
  >();

  for (const line of output.split('\n')) {
    if (!line) continue;
    // Parse "path:linenum:content" or "path:linenum:content"
    const firstColon = line.indexOf(':');
    if (firstColon === -1) continue;
    const secondColon = line.indexOf(':', firstColon + 1);
    if (secondColon === -1) continue;

    const file = line.substring(0, firstColon);
    const lineNum = parseInt(line.substring(firstColon + 1, secondColon), 10);
    const content = line.substring(secondColon + 1);

    if (isNaN(lineNum)) continue;

    let matches = fileMap.get(file);
    if (!matches) {
      matches = [];
      fileMap.set(file, matches);
    }
    matches.push({
      line_number: lineNum,
      line: content,
      column_start: 0,
      column_end: content.length,
      context_before: [],
      context_after: [],
    });
  }

  return Array.from(fileMap.entries()).map(([file, matches]) => ({
    file,
    matches,
    match_count: matches.length,
  }));
}

/**
 * Normalize and validate a file path for WASM operations.
 * Rejects path traversal attempts.
 */
export function sanitizePath(path: string): string {
  // Remove null bytes
  let clean = path.replace(/\0/g, '');
  // Normalize slashes
  clean = clean.replace(/\\/g, '/');
  // Collapse multiple slashes
  clean = clean.replace(/\/+/g, '/');
  // Remove trailing slash (unless root)
  if (clean.length > 1 && clean.endsWith('/')) {
    clean = clean.slice(0, -1);
  }
  // Reject path traversal
  const parts = clean.split('/');
  const resolved: string[] = [];
  for (const part of parts) {
    if (part === '..') {
      if (resolved.length > 0) resolved.pop();
      // Allow traversal at root — just clamp
    } else if (part !== '.' && part !== '') {
      resolved.push(part);
    }
  }
  return '/' + resolved.join('/');
}

/**
 * Extract a query parameter value from a URL string.
 */
export function getQueryParam(url: string, name: string): string | null {
  try {
    let search: string;
    if (url.startsWith('/')) {
      const qIdx = url.indexOf('?');
      search = qIdx === -1 ? '' : url.substring(qIdx);
    } else {
      search = new URL(url).search;
    }
    const params = new URLSearchParams(search);
    return params.get(name);
  } catch {
    return null;
  }
}

/**
 * Safely parse JSON, returning null on failure.
 */
export function safeParseJson(str: string): Record<string, unknown> | null {
  try {
    return JSON.parse(str);
  } catch {
    return null;
  }
}

/** Create a JSON success response. */
export function jsonOk(data: unknown): Response {
  return new Response(JSON.stringify(data), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

/** Create a JSON error response. */
export function jsonError(message: string, status: number): Response {
  return new Response(JSON.stringify({ error: message, message }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}
