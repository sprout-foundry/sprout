import { createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react-dom/test-utils';
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
});

afterEach(() => {
  if (root) root.unmount();
  if (container) container.remove();
});

function makeResult(overrides: Partial<SemanticSearchResult> = {}): SemanticSearchResult {
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

async function renderResults(results: SemanticSearchResult[]) {
  await act(async () => {
    root!.render(
      createElement(SemanticSearchResults, {
        results,
        onFileClick,
        onMouseEnter,
        onMouseLeave,
      }),
    );
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SemanticSearchResults', () => {
  it('renders results with no clusters in original order', async () => {
    const results = [makeResult({ file: '/a.ts', name: 'funcA' }), makeResult({ file: '/b.ts', name: 'funcB' })];
    await renderResults(results);

    const rows = container!.querySelectorAll('.search-semantic-result');
    expect(rows.length).toBe(2);
    expect(rows[0]).toHaveTextContent('funcA');
    expect(rows[1]).toHaveTextContent('funcB');
  });

  it('renders a hint banner before each cluster group', async () => {
    const results = [
      makeResult({ file: '/a.ts', name: 'funcA', cluster_id: 1 }),
      makeResult({ file: '/b.ts', name: 'funcB', cluster_id: 1 }),
      makeResult({ file: '/c.ts', name: 'funcC', cluster_id: 2 }),
      makeResult({ file: '/d.ts', name: 'funcD', cluster_id: 2 }),
    ];
    await renderResults(results);

    const hints = container!.querySelectorAll('.search-duplicate-hint');
    expect(hints.length).toBe(2);

    // First hint should mention 2 results (cluster 1)
    expect(hints[0].textContent).toContain('2 results');
    expect(hints[0].textContent).toContain('cluster 1');
    // Second hint should mention 2 results (cluster 2)
    expect(hints[1].textContent).toContain('2 results');
    expect(hints[1].textContent).toContain('cluster 2');
  });

  it('renders clustered results before non-clustered results', async () => {
    const results = [
      makeResult({ file: '/a.ts', name: 'funcA', cluster_id: 0 }),
      makeResult({ file: '/b.ts', name: 'funcB', cluster_id: 1 }),
      makeResult({ file: '/c.ts', name: 'funcC', cluster_id: 1 }),
    ];
    await renderResults(results);

    const hint = container!.querySelector('.search-duplicate-hint');
    expect(hint).not.toBeNull();

    const rows = container!.querySelectorAll('.search-semantic-result');
    expect(rows.length).toBe(3);
    // Clustered items should appear before non-clustered
    const firstRow = rows[0];
    expect(firstRow.className).toContain('clustered');
  });

  it('renders the hint icon span', async () => {
    const results = [
      makeResult({ file: '/a.ts', name: 'funcA', cluster_id: 1 }),
      makeResult({ file: '/b.ts', name: 'funcB', cluster_id: 1 }),
    ];
    await renderResults(results);

    const icon = container!.querySelector('.search-duplicate-hint-icon');
    expect(icon).not.toBeNull();
    expect(icon!.textContent).toContain('⚠️');
  });

  it('preserves original order within each cluster', async () => {
    const results = [
      makeResult({ file: '/a.ts', name: 'first', cluster_id: 1 }),
      makeResult({ file: '/b.ts', name: 'second', cluster_id: 1 }),
    ];
    await renderResults(results);

    const names = container!.querySelectorAll('.search-semantic-result-name');
    expect(names[0]?.textContent).toBe('first');
    expect(names[1]?.textContent).toBe('second');
  });

  it('handles file-type results within clusters', async () => {
    const results = [
      makeResult({ file: '/a.ts', name: 'funcA', cluster_id: 1 }),
      makeResult({ file: '/b.ts', name: 'funcB', cluster_id: 1, type: 'file' }),
    ];
    await renderResults(results);

    const hint = container!.querySelector('.search-duplicate-hint');
    expect(hint).not.toBeNull();
    expect(hint!.textContent).toContain('2 results');
  });

  it('handles empty results', async () => {
    await renderResults([]);
    expect(container!.innerHTML).toBe('');
  });
});
