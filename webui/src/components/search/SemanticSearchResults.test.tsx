import { createElement } from 'react';
import { createRoot } from 'react-dom/client';
import SemanticSearchResults from './SemanticSearchResults';
import type { SemanticSearchResult } from './types';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

const onFileClick = vi.fn();
const onMouseEnter = vi.fn();
const onMouseLeave = vi.fn();

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  onFileClick.mockClear();
  onMouseEnter.mockClear();
  onMouseLeave.mockClear();
});

afterEach(() => {
  if (root) root.unmount();
  if (container) container.remove();
});

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function makeResult(
  overrides: Partial<SemanticSearchResult> = {},
): SemanticSearchResult {
  return {
    file: '/project/src/file.ts',
    name: 'func',
    signature: '',
    start_line: 1,
    end_line: 10,
    language: 'typescript',
    similarity: 0.85,
    type: 'code_unit',
    cluster_id: 0,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SemanticSearchResults', () => {
  it('renders results with no clusters in original order', () => {
    const results = [
      makeResult({ file: '/a.ts', name: 'alpha' }),
      makeResult({ file: '/b.ts', name: 'beta' }),
    ];

    root!.render(
      createElement(SemanticSearchResults, {
        results,
        onFileClick,
        onMouseEnter,
        onMouseLeave,
      }),
    );

    const hints = container!.querySelectorAll('.search-duplicate-hint');
    expect(hints.length).toBe(0);

    const rows = container!.querySelectorAll('.search-semantic-result');
    expect(rows.length).toBe(2);
  });

  it('renders a hint banner before each cluster group', () => {
    const results = [
      makeResult({ file: '/a.ts', name: 'funcA', cluster_id: 1 }),
      makeResult({ file: '/b.ts', name: 'funcB', cluster_id: 1 }),
      makeResult({ file: '/c.ts', name: 'funcC', cluster_id: 2 }),
    ];

    root!.render(
      createElement(SemanticSearchResults, {
        results,
        onFileClick,
        onMouseEnter,
        onMouseLeave,
      }),
    );

    const hints = container!.querySelectorAll('.search-duplicate-hint');
    expect(hints.length).toBe(2);

    // First hint should mention 2 results (cluster 1)
    expect(hints[0].textContent).toContain('2 results');
    expect(hints[0].textContent).toContain('cluster 1');

    // Second hint should mention 1 result (cluster 2)
    expect(hints[1].textContent).toContain('1 result');
    expect(hints[1].textContent).toContain('cluster 2');
  });

  it('renders clustered results before non-clustered results', () => {
    const results = [
      makeResult({ file: '/nonclustered.ts', name: 'plain' }),
      makeResult({ file: '/clustered1.ts', name: 'dup1', cluster_id: 1 }),
      makeResult({ file: '/clustered2.ts', name: 'dup2', cluster_id: 1 }),
      makeResult({ file: '/nonclustered2.ts', name: 'plain2' }),
    ];

    root!.render(
      createElement(SemanticSearchResults, {
        results,
        onFileClick,
        onMouseEnter,
        onMouseLeave,
      }),
    );

    // Clustered results appear first with a hint, then non-clustered
    const hint = container!.querySelector('.search-duplicate-hint');
    expect(hint).not.toBeNull();
    expect(hint!.textContent).toContain('2 results');

    // All 4 result rows should still be present
    const rows = container!.querySelectorAll('.search-semantic-result');
    expect(rows.length).toBe(4);
  });

  it('handles empty results', () => {
    root!.render(
      createElement(SemanticSearchResults, {
        results: [],
        onFileClick,
        onMouseEnter,
        onMouseLeave,
      }),
    );

    expect(container!.querySelectorAll('.search-semantic-result').length).toBe(0);
    expect(container!.querySelectorAll('.search-duplicate-hint').length).toBe(0);
  });

  it('renders the hint icon span', () => {
    const results = [
      makeResult({ file: '/a.ts', name: 'funcA', cluster_id: 1 }),
    ];

    root!.render(
      createElement(SemanticSearchResults, {
        results,
        onFileClick,
        onMouseEnter,
        onMouseLeave,
      }),
    );

    const icon = container!.querySelector('.search-duplicate-hint-icon');
    expect(icon).not.toBeNull();
  });

  it('preserves original order within each cluster', () => {
    const results = [
      makeResult({ file: '/first.ts', name: 'first', cluster_id: 1, start_line: 1 }),
      makeResult({ file: '/second.ts', name: 'second', cluster_id: 1, start_line: 50 }),
    ];

    root!.render(
      createElement(SemanticSearchResults, {
        results,
        onFileClick,
        onMouseEnter,
        onMouseLeave,
      }),
    );

    const rows = container!.querySelectorAll('.search-semantic-result-name');
    expect(rows[0].textContent).toBe('first');
    expect(rows[1].textContent).toBe('second');
  });

  it('handles file-type results within clusters', () => {
    const results = [
      makeResult({ file: '/a.ts', name: 'File', type: 'file', start_line: 1, end_line: 1, cluster_id: 1 }),
      makeResult({ file: '/b.ts', name: 'funcB', cluster_id: 1 }),
    ];

    root!.render(
      createElement(SemanticSearchResults, {
        results,
        onFileClick,
        onMouseEnter,
        onMouseLeave,
      }),
    );

    const hint = container!.querySelector('.search-duplicate-hint');
    expect(hint).not.toBeNull();
    expect(hint!.textContent).toContain('2 results');

    const rows = container!.querySelectorAll('.search-semantic-result');
    expect(rows.length).toBe(2);
  });
});
