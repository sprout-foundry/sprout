/**
 * E-M3 standalone editor — CodeMirror 6 + the WASM file layer, no React.
 *
 * Host protocol (window.postMessage):
 *   host → page: { source: 'sprout-host', type: 'open',   path, content? }
 *   host → page: { source: 'sprout-host', type: 'theme',  bg?, fg? }
 *   host → page: { source: 'sprout-host', type: 'setDoc', content }  // programmatic edit
 *   host → page: { source: 'sprout-host', type: 'save' }
 *   page → host: { source: 'sprout-editor', type: 'ready' }
 *   page → host: { source: 'sprout-editor', type: 'dirty', value: bool }
 *   page → host: { source: 'sprout-editor', type: 'save', path, content }
 *                (host ACKs with { type: 'saved', path } to clear dirty)
 *
 * Persistence: reads/writes through the wasmShell service — the same
 * SproutWasm-backed store the webui uses (sprout-wasm-fs IndexedDB/OPFS),
 * so files edited here are visible to the terminal component and the
 * webui alike.
 */
import { EditorView } from '@codemirror/view';
import { editorExtensionsFor, languageTitleFor } from './editorLang';
import type { WasmShell } from '../../services/wasmShell';

const statusPath = document.querySelector<HTMLSpanElement>('#status .path')!;
const statusState = document.querySelector<HTMLSpanElement>('#status .state')!;
const statusBox = document.getElementById('status')!;

let view: EditorView | null = null;
let currentPath: string | null = null;
let dirty = false;
let shellReady = false;
let wasm: WasmShell | null = null;

function setState(text: string, isDirty = false) {
  statusState.textContent = text;
  statusBox.classList.toggle('dirty', isDirty);
}

function post(type: string, payload: Record<string, unknown> = {}) {
  window.parent?.postMessage({ source: 'sprout-editor', type, ...payload }, '*');
}

async function boot() {
  // Lazy-import the WASM shell so the editor chrome paints first.
  setState('loading runtime…');
  try {
    const mod = await import('../../services/wasmShell');
    wasm = await mod.initWasmShell({});
    shellReady = true;
    setState('ready');
    post('ready');
  } catch (err) {
    setState('wasm failed');
    console.error('[editor] wasm init failed:', err);
    // Still functional as a plain editor (no persistence).
    post('ready');
  }
}

function markDirty() {
  if (!dirty) {
    dirty = true;
    setState('unsaved changes', true);
    post('dirty', { value: true });
  }
}

function clearDirty() {
  dirty = false;
  setState('saved', false);
  post('dirty', { value: false });
}

function buildView(doc: string, path: string) {
  const host = document.getElementById('editor')!;
  host.innerHTML = '';
  const exts = [
    ...editorExtensionsFor(path),
    EditorView.updateListener.of((u) => {
      if (u.docChanged) markDirty();
    }),
  ];
  view = new EditorView({ doc, extensions: exts, parent: host });
}

async function openPath(path: string, content?: string) {
  currentPath = path;
  statusPath.textContent = `${path} · ${languageTitleFor(path)}`;
  let doc = content ?? '';
  if (content === undefined && shellReady && wasm) {
    const res = wasm.readFile(path);
    if (!res.error) doc = res.content;
  }
  buildView(doc, path);
  clearDirty();
}
async function saveCurrent() {
  if (!view || !currentPath) return;
  const content = view.state.doc.toString();
  if (shellReady && wasm) {
    const err = wasm.writeFile(currentPath, content);
    if (err) {
      console.error('[editor] save failed:', err);
      setState('save failed', true);
      return;
    }
  }
  post('save', { path: currentPath, content });
  clearDirty();
}

// Build an empty scratch view immediately so the editor is usable (and
// visible) before any host `open` arrives; typing marks it dirty and
// ⌘S saves to the scratch path.
currentPath = 'scratch.txt';
statusPath.textContent = 'scratch.txt · Plain Text';
buildView('', 'scratch.txt');

window.addEventListener('message', (ev: MessageEvent) => {
  const data = ev.data;
  if (!data || data.source !== 'sprout-host') return;
  switch (data.type) {
    case 'open':
      openPath(data.path as string, data.content as string | undefined);
      break;
    case 'saved':
      clearDirty();
      break;
    case 'theme':
      if (data.bg) document.documentElement.style.setProperty('--editor-bg', data.bg as string);
      if (data.fg) document.documentElement.style.setProperty('--editor-fg', data.fg as string);
      break;
    case 'setDoc':
      // Programmatic edit (host-driven). Tests + the native host use this
      // instead of simulating keystrokes.
      if (view) {
        view.dispatch({
          changes: { from: 0, to: view.state.doc.length, insert: String(data.content ?? '') },
        });
      }
      break;
    case 'save':
      saveCurrent();
      break;
  }
});

// Keyboard: ⌘S / Ctrl+S saves even when the host hasn't wired a button.
window.addEventListener('keydown', (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
    e.preventDefault();
    saveCurrent();
  }
});

boot();
