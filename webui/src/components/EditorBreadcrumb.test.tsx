// @ts-nocheck

import React from 'react';
import { createRoot } from 'react-dom/client';
import { act } from 'react';
import EditorBreadcrumb from './EditorBreadcrumb';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('lucide-react', () => ({
  ChevronRight: (props: any) => <svg data-testid="chevron-right" {...props} />,
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: ReturnType<typeof createRoot>;

function renderBreadcrumb(
  props: Partial<{
    filePath: string;
    onNavigate?: (path: string) => void;
  }> = {},
) {
  const {
    filePath = 'src/components/App.tsx',
    onNavigate,
  } = props;

  act(() => {
    root.render(
      <EditorBreadcrumb
        filePath={filePath}
        onNavigate={onNavigate}
      />,
    );
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

// ── Null/empty rendering ──

describe('EditorBreadcrumb null rendering', () => {
  test('returns null for virtual workspace paths starting with __workspace/', () => {
    renderBreadcrumb({ filePath: '__workspace/chat' });
    expect(container.querySelector('.editor-breadcrumb')).toBeNull();
  });

  test('returns null for empty string filePath', () => {
    renderBreadcrumb({ filePath: '' });
    expect(container.querySelector('.editor-breadcrumb')).toBeNull();
  });

  test('returns null for plain filename without directory separator', () => {
    renderBreadcrumb({ filePath: 'file.ts' });
    expect(container.querySelector('.editor-breadcrumb')).toBeNull();
  });

  test('returns null for single-directory path (only one segment after filtering)', () => {
    renderBreadcrumb({ filePath: 'src/' });
    expect(container.querySelector('.editor-breadcrumb')).toBeNull();
  });

  test('returns null for path with only one non-empty segment', () => {
    renderBreadcrumb({ filePath: 'src' });
    expect(container.querySelector('.editor-breadcrumb')).toBeNull();
  });
});

// ── Rendering breadcrumb segments ──

describe('EditorBreadcrumb segment rendering', () => {
  test('renders all segments for "src/components/App.tsx"', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(3);
    expect(segments[0].textContent).toBe('src');
    expect(segments[1].textContent).toBe('components');
    expect(segments[2].textContent).toBe('App.tsx');
  });

  test('renders the breadcrumb as a nav element with aria-label', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });
    const nav = container.querySelector('nav.editor-breadcrumb');
    expect(nav).not.toBeNull();
    expect(nav?.getAttribute('aria-label')).toBe('Breadcrumb');
  });

  test('renders an ol list inside the nav', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });
    const list = container.querySelector('.breadcrumb-list');
    expect(list).not.toBeNull();
    expect(list?.tagName.toLowerCase()).toBe('ol');
  });

  test('last segment has breadcrumb-segment-current class and aria-current', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments[2].classList.contains('breadcrumb-segment-current')).toBe(true);
    expect(segments[2].getAttribute('aria-current')).toBe('page');
  });

  test('non-current segments are rendered as buttons', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    // First two segments should be <button> elements
    expect(segments[0].tagName.toLowerCase()).toBe('button');
    expect(segments[1].tagName.toLowerCase()).toBe('button');
    // Last segment should be <span>
    expect(segments[2].tagName.toLowerCase()).toBe('span');
  });

  test('renders ChevronRight separators between segments', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const separators = container.querySelectorAll('.breadcrumb-separator');
    expect(separators).toHaveLength(2);
  });

  test('renders correct number of separators for a 2-segment path', () => {
    renderBreadcrumb({ filePath: 'src/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(2);

    const separators = container.querySelectorAll('.breadcrumb-separator');
    expect(separators).toHaveLength(1);
  });

  test('separators have aria-hidden="true"', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const separators = container.querySelectorAll('.breadcrumb-separator');
    separators.forEach((sep) => {
      expect(sep.getAttribute('aria-hidden')).toBe('true');
    });
  });
});

// ── Title attributes ──

describe('EditorBreadcrumb title attributes', () => {
  test('non-current segments have title showing path up to that segment', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments[0].getAttribute('title')).toBe('src');
    expect(segments[1].getAttribute('title')).toBe('src/components');
  });

  test('title for nested directory path with 4 levels', () => {
    renderBreadcrumb({
      filePath: 'src/features/auth/LoginForm.tsx',
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(4);
    expect(segments[0].getAttribute('title')).toBe('src');
    expect(segments[1].getAttribute('title')).toBe('src/features');
    expect(segments[2].getAttribute('title')).toBe('src/features/auth');
  });
});

// ── Click handling ──

describe('EditorBreadcrumb click handling', () => {
  test('clicking non-current segment calls onNavigate with correct path', () => {
    const onNavigate = jest.fn();
    renderBreadcrumb({
      filePath: 'src/components/App.tsx',
      onNavigate,
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');

    act(() => {
      (segments[0] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledTimes(1);
    expect(onNavigate).toHaveBeenCalledWith('src');

    act(() => {
      (segments[1] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledTimes(2);
    expect(onNavigate).toHaveBeenCalledWith('src/components');
  });

  test('clicking the current (last) span segment does NOT cause errors', () => {
    const onNavigate = jest.fn();
    renderBreadcrumb({
      filePath: 'src/components/App.tsx',
      onNavigate,
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    const lastSegment = segments[2] as HTMLElement;

    // Last segment is a <span>, clicking it is safe
    act(() => {
      lastSegment.click();
    });

    expect(onNavigate).not.toHaveBeenCalled();
  });

  test('clicking segments when onNavigate is not provided does not throw', () => {
    renderBreadcrumb({
      filePath: 'src/components/App.tsx',
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');

    expect(() => {
      act(() => {
        (segments[0] as HTMLElement).click();
        (segments[1] as HTMLElement).click();
        (segments[2] as HTMLElement).click();
      });
    }).not.toThrow();
  });

  test('clicking all non-current segments with a 4-level path calls onNavigate correctly', () => {
    const onNavigate = jest.fn();
    renderBreadcrumb({
      filePath: 'src/features/auth/LoginForm.tsx',
      onNavigate,
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(4);

    act(() => {
      (segments[0] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledWith('src');

    act(() => {
      (segments[1] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledWith('src/features');

    act(() => {
      (segments[2] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledWith('src/features/auth');

    // Current segment (span) — should not call
    act(() => {
      (segments[3] as HTMLElement).click();
    });
    expect(onNavigate).toHaveBeenCalledTimes(3);
  });

  test('keyboard activation with Enter key calls onNavigate', () => {
    const onNavigate = jest.fn();
    renderBreadcrumb({
      filePath: 'src/components/App.tsx',
      onNavigate,
    });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    const firstSegment = segments[0] as HTMLElement;

    act(() => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true });
      firstSegment.dispatchEvent(event);
    });

    expect(onNavigate).toHaveBeenCalledWith('src');
  });
});

// ── Edge cases ──

describe('EditorBreadcrumb edge cases', () => {
  test('handles multiple consecutive slashes correctly', () => {
    renderBreadcrumb({ filePath: 'src//components///App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(3);
    expect(segments[0].textContent).toBe('src');
    expect(segments[1].textContent).toBe('components');
    expect(segments[2].textContent).toBe('App.tsx');
  });

  test('handles path starting with a slash (leading slash filtered out)', () => {
    renderBreadcrumb({ filePath: '/src/components/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(3);
    expect(segments[0].textContent).toBe('src');
  });

  test('handles path with trailing slash (trailing empty string filtered out)', () => {
    renderBreadcrumb({ filePath: 'src/components/App.tsx/' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(3);
    expect(segments[2].textContent).toBe('App.tsx');
    expect(segments[2].classList.contains('breadcrumb-segment-current')).toBe(true);
  });

  test('path with only two segments renders both', () => {
    renderBreadcrumb({ filePath: 'src/App.tsx' });

    const segments = container.querySelectorAll('.breadcrumb-segment');
    expect(segments).toHaveLength(2);
    expect(segments[0].textContent).toBe('src');
    expect(segments[1].textContent).toBe('App.tsx');

    const separators = container.querySelectorAll('.breadcrumb-separator');
    expect(separators).toHaveLength(1);
  });
});
