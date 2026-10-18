// @ts-nocheck
// New-file (Ctrl+N "Untitled") flow:
//   - an untitled buffer is virtual: it must never be polled as a disk file
//     (the "file changed externally" dialog loop before the first save)
//   - Save As on an untitled buffer must resolve the typed path against the
//     WORKSPACE root, not the process cwd / user home
//   - Save As must adopt the real path so the tab stops reading "Untitled"
//     and the file shows up in the workspace tree
import { showThemedPrompt } from '@sprout/ui';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import { checkFilesModified } from '../services/apiFileCheck';
import { getLSPClientService } from '../services/lspClientService';
import { EditorManagerProvider, useEditorManager } from './EditorManagerContext';

const sproutFetchMock = vi.fn();

vi.mock('./SproutAdapterContext', () => ({
  SproutAdapterProvider: ({ children }) => children,
  useSproutAdapter: () => ({ clientFetch: vi.fn() }),
  useSproutFetch: () => sproutFetchMock,
}));

vi.mock('../services/apiFileCheck', () => ({
  checkFilesModified: vi.fn(),
}));

vi.mock('@sprout/ui', async (importOriginal) => {
  const actual = await importOriginal();
  return { ...actual, showThemedPrompt: vi.fn() };
});

vi.mock('./EditorSettingsContext', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    useEditorSettings: () => ({
      ...actual.useEditorSettings(),
      isAutoSaveEnabled: false,
      isFormatOnSaveEnabled: false,
    }),
  };
});

const mockCheckFilesModified = checkFilesModified as ReturnType<typeof vi.fn>;
const mockPrompt = showThemedPrompt as ReturnType<typeof vi.fn>;

let container: HTMLDivElement;
let root: Root;
let latestContext: any;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  vi.clearAllMocks();
  latestContext = undefined;
  mockCheckFilesModified.mockResolvedValue({ modified: [] });
  // The workspace root is what a typed Save As path resolves against; seed
  // it the way boot does (layout restore / workspace fetch).
  getLSPClientService().setWorkspaceRoot('/w');
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  localStorage.removeItem('sprout.editor.layoutState');
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
  vi.restoreAllMocks();
});

function TestConsumer() {
  const ctx = useEditorManager();
  latestContext = ctx;
  return null;
}

const ctx = () => latestContext;

function renderProvider() {
  // Rendering a React root — not a Testing Library util call.
  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => {
    root.render(createElement(EditorManagerProvider, null, createElement(TestConsumer)));
  });
}

async function actAndUpdate(fn: () => void) {
  act(() => {
    fn();
  });
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function okWriteResponse(path: string) {
  return new Response(
    JSON.stringify({ success: true, message: 'File saved successfully', path, size: 3, mod_time: 5000 }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
}

async function openUntitled() {
  renderProvider();
  let bufferId: string | null = null;
  await actAndUpdate(() => {
    bufferId = ctx().openWorkspaceBuffer({
      kind: 'file',
      path: `__workspace/untitled-${Date.now()}`,
      title: 'Untitled',
      ext: '',
      isClosable: true,
    });
  });
  return bufferId;
}

describe('untitled (Ctrl+N) buffers', () => {
  it('is never polled as a disk file before it has a real path', async () => {
    await openUntitled();
    await actAndUpdate(() => {
      ctx().updateBufferContent(ctx().buffers.keys().next().value, 'hello');
    });

    await new Promise((r) => setTimeout(r, 800));

    expect(mockCheckFilesModified).not.toHaveBeenCalled();
  });

  it('Save As resolves the typed relative path against the workspace root', async () => {
    const bufferId = await openUntitled();
    await actAndUpdate(() => {
      ctx().updateBufferContent(bufferId, 'hello');
    });

    mockPrompt.mockResolvedValue('src/new.ts');
    const urls: string[] = [];
    sproutFetchMock.mockImplementation(async (url) => {
      urls.push(String(url));
      return okWriteResponse('/w/src/new.ts');
    });

    await actAndUpdate(() => {
      ctx().saveBuffer(bufferId);
    });

    expect(urls).toHaveLength(1);
    const sent = decodeURIComponent(urls[0]);
    expect(sent).toContain('path=/w/src/new.ts');
    expect(sent).not.toContain('path=src/new.ts');
  });

  it('adopts the saved path so the tab stops reading Untitled', async () => {
    const bufferId = await openUntitled();
    await actAndUpdate(() => {
      ctx().updateBufferContent(bufferId, 'hello');
    });

    mockPrompt.mockResolvedValue('src/new.ts');
    sproutFetchMock.mockImplementation(async () => okWriteResponse('/w/src/new.ts'));

    await actAndUpdate(() => {
      ctx().saveBuffer(bufferId);
    });

    const buf = ctx().buffers.get(bufferId);
    expect(buf.file.path).toBe('/w/src/new.ts');
    expect(buf.file.name).toBe('new.ts');
    expect(buf.file.ext).toBe('.ts');
    expect(buf.isModified).toBe(false);
    expect(buf.originalContent).toBe('hello');
  });
});
