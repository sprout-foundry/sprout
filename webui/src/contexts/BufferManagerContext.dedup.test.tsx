// @ts-nocheck
// openFile dedup: the same file reached through different path spellings
// (tree, search results, terminal links) must reuse one buffer.
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';
import { notificationBus } from '../services/notificationBus';
import { EditorManagerProvider, useEditorManager } from './EditorManagerContext';

const sproutFetchMock = vi.fn();

vi.mock('./SproutAdapterContext', () => ({
  SproutAdapterProvider: ({ children }) => children,
  useSproutAdapter: () => ({ clientFetch: vi.fn() }),
  useSproutFetch: () => sproutFetchMock,
}));

// Format-on-save toggled per-test through this handle.
const formatSettings = { enabled: false, formatted: 'FORMATTED' };

vi.mock('./EditorSettingsContext', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    useEditorSettings: () => ({
      ...actual.useEditorSettings(),
      isAutoSaveEnabled: false,
      isFormatOnSaveEnabled: formatSettings.enabled,
    }),
  };
});

vi.mock('../services/formatter', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    formatCodeWithConfigDiscovery: vi.fn(async (_content: string) => ({
      formatted: formatSettings.formatted,
      error: undefined,
    })),
    isFormattable: vi.fn(() => true),
  };
});

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
  formatSettings.enabled = false;
  formatSettings.formatted = 'FORMATTED';
  latestContext = undefined;
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
  // Flush React batching + microtasks (not a Testing Library util call,
  // so the testing-library act lint rule does not apply here).
  // eslint-disable-next-line testing-library/no-unnecessary-act
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe('openFile dedup', () => {
  it('reuses the buffer for another spelling of the same path and stores it normalized', async () => {
    renderProvider();
    let first = '';
    let second = '';
    await actAndUpdate(() => {
      first = ctx().openFile({ path: 'src\\a.ts', name: 'a.ts', isDirectory: false });
    });
    await actAndUpdate(() => {
      second = ctx().openFile({ path: 'src/a.ts', name: 'a.ts', isDirectory: false });
    });
    expect(second).toBe(first);
    const fileBuffers = [...ctx().buffers.values()].filter((b) => b.kind === 'file');
    expect(fileBuffers).toHaveLength(1);
    expect(fileBuffers[0].file.path).toBe('src/a.ts');
  });
});
