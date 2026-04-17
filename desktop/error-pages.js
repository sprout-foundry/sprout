/**
 * Error and loading page renderers for the Electron main process.
 * These produce data: URLs that BrowserWindow.loadURL() can display
 * without a running backend.
 */

const fs = require('node:fs');
const path = require('node:path');
const { getLogDirectory } = require('./state-manager');

function htmlEscape(str) {
  return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function renderLoadingPage(workspacePath) {
  return `data:text/html;charset=UTF-8,${encodeURIComponent(`
    <!doctype html>
    <html>
      <body style="margin:0;font-family:sans-serif;background:#1f242d;color:#d6deeb;display:flex;align-items:center;justify-content:center;height:100vh;">
        <div style="text-align:center;">
          <div style="font-size:18px;font-weight:600;margin-bottom:8px;">Starting Ledit…</div>
          <div style="font-size:13px;opacity:.75;">${htmlEscape(workspacePath)}</div>
        </div>
      </body>
    </html>
  `)}`;
}

function getRecentLogLines(maxLines = 40) {
  try {
    const logDir = getLogDirectory();
    const files = fs.readdirSync(logDir)
      .filter((f) => f.startsWith('backend-') && f.endsWith('.log'))
      .map((f) => ({ name: f, mtime: fs.statSync(path.join(logDir, f)).mtimeMs }))
      .sort((a, b) => b.mtime - a.mtime);
    if (files.length === 0) return { lines: [], logPath: null };
    const logPath = path.join(logDir, files[0].name);
    const content = fs.readFileSync(logPath, 'utf8');
    const lines = content.split('\n').filter(Boolean);
    return { lines: lines.slice(-maxLines), logPath };
  } catch {
    return { lines: [], logPath: null };
  }
}

function likelyCause(exitCode, signal) {
  if (signal === 'SIGKILL') return 'The backend process was killed by the OS (possibly OOM or force-quit).';
  if (signal === 'SIGSEGV') return 'The backend process crashed with a segmentation fault.';
  if (signal) return `The backend process was terminated by signal ${signal}.`;
  if (exitCode === 1) return 'The backend exited with an error. Check the log for details.';
  if (exitCode === 2) return 'The backend could not start — a required resource or permission is missing.';
  if (exitCode === 127) return 'The backend binary was not found. Try reinstalling.';
  if (exitCode !== null && exitCode !== undefined) return `The backend exited with code ${exitCode}.`;
  return 'The backend stopped unexpectedly.';
}

function renderErrorPage(workspacePath, exitCode, signal, retriesExhausted = false) {
  const { lines: logLines, logPath } = getRecentLogLines(40);
  const logDir = logPath ? path.dirname(logPath) : getLogDirectory();
  const safeWorkspacePath = htmlEscape(workspacePath);

  const exitInfo = [];
  if (exitCode !== null && exitCode !== undefined) exitInfo.push(`Exit code: ${exitCode}`);
  if (signal !== null && signal !== undefined) exitInfo.push(`Signal: ${signal}`);
  const exitDetails = exitInfo.length > 0
    ? `<div class="meta">${exitInfo.join(' &bull; ')}</div>`
    : '';

  const cause = likelyCause(exitCode, signal);

  const heading = retriesExhausted
    ? 'Backend process could not be restarted'
    : 'Backend process exited unexpectedly';

  const reloadButton = retriesExhausted
    ? '<button disabled>Reload</button>'
    : '<button id="reloadBtn">Reload</button>';

  const logHtml = logLines.length > 0
    ? `<details><summary>Recent log (${logLines.length} lines)</summary><pre id="logPre">${logLines.map((l) => l.replace(/&/g, '&amp;').replace(/</g, '&lt;')).join('\n')}</pre></details>`
    : '<p class="meta">No log output available.</p>';

  const diagnosticsText = JSON.stringify({
    exitCode,
    signal,
    cause,
    workspace: workspacePath,
    recentLog: logLines,
  }, null, 2);

  return `data:text/html;charset=UTF-8,${encodeURIComponent(`
    <!doctype html>
    <html>
      <head>
        <style>
          * { box-sizing: border-box; }
          body { margin: 0; font-family: system-ui, sans-serif; background: #1f242d; color: #d6deeb; display: flex; align-items: center; justify-content: center; min-height: 100vh; padding: 24px; }
          .card { max-width: 640px; width: 100%; }
          h2 { font-size: 18px; font-weight: 600; margin: 0 0 8px; color: #ff6b6b; }
          .cause { font-size: 14px; margin-bottom: 12px; color: #c8d0e0; }
          .meta { font-size: 12px; color: #8a9ab5; margin-bottom: 12px; }
          details { margin: 12px 0; }
          summary { cursor: pointer; font-size: 13px; color: #8a9ab5; user-select: none; }
          summary:hover { color: #c8d0e0; }
          pre { background: #161b22; border: 1px solid #2d3748; border-radius: 4px; padding: 10px; font-size: 11px; line-height: 1.5; max-height: 220px; overflow-y: auto; white-space: pre-wrap; word-break: break-all; margin: 6px 0 0; color: #a8b2c8; }
          .actions { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 16px; }
          button { background: #4a9eff; color: white; border: none; padding: 9px 16px; font-size: 13px; border-radius: 4px; cursor: pointer; }
          button:hover { background: #3a8eef; }
          button.secondary { background: #2d3748; color: #c8d0e0; }
          button.secondary:hover { background: #3a4a5e; }
          button[disabled] { background: #3a4050; color: #6a7080; cursor: not-allowed; }
          .log-path { font-size: 11px; color: #6a7890; margin-top: 10px; word-break: break-all; }
        </style>
      </head>
      <body>
        <div class="card">
          <h2>${heading}</h2>
          <p class="cause">${cause}</p>
          ${exitDetails}
          <div class="meta">Workspace: ${safeWorkspacePath}</div>
          ${logHtml}
          <div class="actions">
            ${reloadButton}
            <button class="secondary" id="copyBtn">Copy Diagnostics</button>
            <button class="secondary" id="logsBtn">Open Log Folder</button>
          </div>
          ${logPath ? `<div class="log-path">Log: ${htmlEscape(logPath)}</div>` : ''}
        </div>
        <script>
          const diagnostics = ${JSON.stringify(diagnosticsText)};
          const logDir = ${JSON.stringify(logDir)};
          document.getElementById('reloadBtn') && document.getElementById('reloadBtn').addEventListener('click', () => { location.href = 'ledit://reload'; });
          document.getElementById('copyBtn').addEventListener('click', () => { navigator.clipboard.writeText(diagnostics).then(() => { document.getElementById('copyBtn').textContent = 'Copied!'; setTimeout(() => { document.getElementById('copyBtn').textContent = 'Copy Diagnostics'; }, 2000); }); });
          document.getElementById('logsBtn').addEventListener('click', () => { location.href = 'ledit://open-log-dir?dir=' + encodeURIComponent(logDir); });
        </script>
      </body>
    </html>
  `)}`;
}

module.exports = { renderLoadingPage, getRecentLogLines, likelyCause, renderErrorPage };
