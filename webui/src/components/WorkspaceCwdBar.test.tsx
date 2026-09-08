/**
 * WorkspaceCwdBar.test.tsx — the Files workspace row (cwd select + add-repo).
 *
 * Covers the contract that keeps the row non-duplicative and honest:
 *  - one option per cloned repo, plus "Workspace root" ('' value)
 *  - NO duplicate chip: the select is the single surface showing the cwd
 *  - a cwd inside a repo (terminal `cd`) appears as a dynamic
 *    "owner/name › sub/path" option instead of the select lying "root"
 *  - selecting an option calls onChange with the workspace-relative value
 *  - the add-repo button only renders when a trigger is provided
 *  - pure helper edge cases (malformed repo refs skipped, deep paths)
 */

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import WorkspaceCwdBar, { buildCwdOptions } from './WorkspaceCwdBar';

// repoDir lives in workspaceGit (pure helper); no mocking needed.

let container: HTMLDivElement;
let root: Root;

function renderBar(props: Partial<Parameters<typeof WorkspaceCwdBar>[0]> = {}) {
  const full = {
    cwd: '',
    repos: [] as string[],
    onChange: vi.fn(),
    ...props,
  } as Parameters<typeof WorkspaceCwdBar>[0];
  act(() => {
    root.render(<WorkspaceCwdBar {...full} />);
  });
  return full;
}

describe('buildCwdOptions (pure)', () => {
  it('maps repos to owner/name select entries', () => {
    const options = buildCwdOptions('', ['octo/sprout', 'alan/tools']);
    expect(options).toEqual([
      { value: 'repos/octo/sprout', label: 'octo/sprout' },
      { value: 'repos/alan/tools', label: 'alan/tools' },
    ]);
  });

  it('skips malformed repo refs instead of throwing', () => {
    const options = buildCwdOptions('', ['not-owner-slash', 'octo/sprout']);
    expect(options.map((o) => o.label)).toEqual(['octo/sprout']);
  });

  it('adds no dynamic option when cwd is empty (root) or a repo root', () => {
    expect(buildCwdOptions('', ['octo/sprout'])).toHaveLength(1);
    const atRepo = buildCwdOptions('repos/octo/sprout', ['octo/sprout']);
    expect(atRepo).toHaveLength(1); // select already covers it — no duplicate
  });

  it('synthesizes a dynamic option for a cwd inside a repo, repo-relative', () => {
    const options = buildCwdOptions('repos/octo/sprout/src/lib', ['octo/sprout']);
    expect(options).toEqual([
      { value: 'repos/octo/sprout', label: 'octo/sprout' },
      { value: 'repos/octo/sprout/src/lib', label: 'octo/sprout › src/lib' },
    ]);
  });

  it('falls back to the raw path for a cwd outside any repo', () => {
    const options = buildCwdOptions('notes/deep/dir', ['octo/sprout']);
    expect(options.at(-1)).toEqual({ value: 'notes/deep/dir', label: 'notes/deep/dir' });
  });
});

describe('WorkspaceCwdBar (render)', () => {
  beforeEach(() => {
    (globalThis as unknown as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    delete (globalThis as unknown as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT;
  });

  it('renders the root option selected with no repos and no chip', () => {
    renderBar({});
    const select = container.querySelector<HTMLSelectElement>('[data-testid="workspace-cwd-select"]')!;
    expect(select).not.toBeNull();
    expect(select.value).toBe('');
    expect(select.options).toHaveLength(1);
    expect(select.options[0].textContent).toBe('Workspace root');
    // The deduplication guarantee: no companion chip repeating the value.
    expect(container.querySelector('[data-testid="workspace-cwd-chip"]')).toBeNull();
  });

  it('shows the selected repo and fires onChange with its path', () => {
    const props = renderBar({ cwd: 'repos/octo/sprout', repos: ['octo/sprout'] });
    const select = container.querySelector<HTMLSelectElement>('[data-testid="workspace-cwd-select"]')!;
    expect(select.value).toBe('repos/octo/sprout');
    const option = select.selectedOptions[0];
    expect(option.textContent).toBe('octo/sprout');
    act(() => {
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    // Simulate picking the root option.
    act(() => {
      Object.defineProperty(select, 'value', { value: '', configurable: true });
      select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    expect(props.onChange).toHaveBeenCalledWith('');
  });

  it('keeps a cwd inside a repo selectable via the dynamic option', () => {
    renderBar({ cwd: 'repos/octo/sprout/src', repos: ['octo/sprout'] });
    const select = container.querySelector<HTMLSelectElement>('[data-testid="workspace-cwd-select"]')!;
    expect(select.value).toBe('repos/octo/sprout/src');
    const labels = Array.from(select.options).map((o) => o.textContent);
    expect(labels).toEqual(['Workspace root', 'octo/sprout', 'octo/sprout › src']);
  });

  it('renders the add-repo button only when a trigger is provided', () => {
    renderBar({});
    expect(container.querySelector('[data-testid="workspace-add-repo-btn"]')).toBeNull();
    renderBar({ onAddRepo: vi.fn(), addRepoDisabled: false });
    expect(container.querySelector('[data-testid="workspace-add-repo-btn"]')).not.toBeNull();
  });

  it('fires onAddRepo on click and respects the disabled state', () => {
    const onAddRepo = vi.fn();
    renderBar({ onAddRepo, addRepoDisabled: true });
    const btn = container.querySelector<HTMLButtonElement>('[data-testid="workspace-add-repo-btn"]')!;
    expect(btn.disabled).toBe(true);
  });
});
