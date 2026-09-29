// @ts-nocheck
// Tabs follow files that move or disappear on disk.
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { movedBufferPath } from './bufferPathSync';
import { EditorManagerProvider, useBufferManager, useEditorManager } from './EditorManagerContext';

vi.mock('./SproutAdapterContext', () => ({
  SproutAdapterProvider: ({ children }) => children,
  useSproutAdapter: () => ({ clientFetch: vi.fn() }),
  useSproutFetch: () => vi.fn(),
}));

let container: HTMLDivElement;
let root: Root;
let latest: any;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  localStorage.setItem('sprout-welcome-dismissed', 'true');
  Object.keys(localStorage)
    .filter((k) => k.startsWith('sprout.editor.'))
    .forEach((k) => localStorage.removeItem(k));
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

function Consumer() {
  latest = { ...useEditorManager(), ...useBufferManager() };
  return null;
}

async function step(fn: () => void) {
  act(fn);
  // eslint-disable-next-line testing-library/no-unnecessary-act
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

const filePaths = () =>
  [...latest.buffers.values()]
    .filter((b) => b.kind === 'file')
    .map((b) => b.file.path)
    .sort();

async function openFiles(...paths: string[]) {
  // eslint-disable-next-line testing-library/no-unnecessary-act
  act(() => root.render(createElement(EditorManagerProvider, null, createElement(Consumer))));
  for (const path of paths) {
    await step(() => latest.openFile({ path, name: path.split('/').pop(), isDir: false, size: 0, modified: 0 }));
  }
}

describe('buffer path sync', () => {
  it('keeps every buffer when files open in the same millisecond', async () => {
    vi.spyOn(Date, 'now').mockReturnValue(1_000);
    await openFiles('src/a.ts', 'src/b.ts');
    vi.restoreAllMocks();
    expect(filePaths()).toEqual(['src/a.ts', 'src/b.ts']);
  });

  it('retargets tabs for a renamed file and for files inside a renamed folder', async () => {
    await openFiles('src/a.ts', 'src/lib/b.ts', 'other.ts');
    await step(() => latest.retargetBufferPaths('src/a.ts', 'src/renamed.ts'));
    await step(() => latest.retargetBufferPaths('src/lib', 'src/util'));
    expect(filePaths()).toEqual(['other.ts', 'src/renamed.ts', 'src/util/b.ts']);
    const renamed = [...latest.buffers.values()].find((b) => b.file?.path === 'src/renamed.ts');
    expect(renamed.file.name).toBe('renamed.ts');
    expect(renamed.file.ext).toBe('.ts');
  });

  it('closes clean tabs under a deleted folder and keeps edited ones', async () => {
    await openFiles('src/a.ts', 'src/b.ts', 'keep.ts');
    const edited = [...latest.buffers.values()].find((b) => b.file?.path === 'src/b.ts');
    await step(() => latest.setBufferModified(edited.id, true));
    await step(() => latest.closeBuffersForDeletedPath('src'));
    expect(filePaths()).toEqual(['keep.ts', 'src/b.ts']);
  });
});

describe('movedBufferPath', () => {
  it('does not treat a sibling with a shared prefix as inside the folder', () => {
    expect(movedBufferPath('src/lib2/x.ts', 'src/lib', 'src/util')).toBeNull();
  });
});
