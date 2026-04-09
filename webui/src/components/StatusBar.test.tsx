import { createRoot } from 'react-dom/client';
import { act } from 'react';
import StatusBar from './StatusBar';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

jest.mock('../extensions/languageRegistry', () => {
  const mockEntries: Array<{ id: string; name: string; extensions: string[] }> = [
    { id: 'typescript', name: 'TypeScript', extensions: ['ts'] },
    { id: 'typescript-jsx', name: 'TypeScript (JSX)', extensions: ['tsx'] },
    { id: 'javascript', name: 'JavaScript', extensions: ['js', 'mjs', 'cjs'] },
    { id: 'go', name: 'Go', extensions: ['go'] },
    { id: 'python', name: 'Python', extensions: ['py'] },
    { id: 'json', name: 'JSON', extensions: ['json'] },
    { id: 'css', name: 'CSS', extensions: ['css'] },
    { id: 'html', name: 'HTML', extensions: ['html', 'htm'] },
    { id: 'plaintext', name: 'Plain Text', extensions: ['txt'] },
  ];
  return {
    allLanguageEntries: mockEntries,
    resolveLanguageId: (override: string | null | undefined, ext?: string, _fileName?: string) => {
      if (override != null && override !== '') {
        return { languageId: override, isAutoDetected: false };
      }
      const match = mockEntries.find(
        (e) => ext && e.extensions.includes(ext.toLowerCase()),
      );
      return { languageId: match?.id ?? null, isAutoDetected: !!match };
    },
  };
});

jest.mock('lucide-react', () => ({
  GitBranch: (props: any) => (
    <svg data-testid="git-branch-icon" {...props} />
  ),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeBuffer(overrides: Record<string, any> = {}) {
  return {
    kind: 'file',
    file: { name: 'app.tsx', ext: '.tsx' },
    content: 'hello world\n',
    cursorPosition: { line: 0, column: 0 },
    languageOverride: null,
    ...overrides,
  };
}

let container: HTMLDivElement;
let root: ReturnType<typeof createRoot>;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => { root.unmount(); });
  container.remove();
});

/** helper: find first element whose textContent exactly matches `text`. */
function getByText(parent: HTMLElement, text: string) {
  const els = Array.from(parent.querySelectorAll('*'));
  const match = els.find((el) => el.textContent === text && el.children.length === 0);
  if (!match) throw new Error(`Unable to find an element with text: "${text}"`);
  return match;
}

/** helper: find element by text, return null if not found. */
function queryByText(parent: HTMLElement, text: string) {
  const els = Array.from(parent.querySelectorAll('*'));
  return els.find((el) => el.textContent === text && el.children.length === 0) ?? null;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('StatusBar', () => {
  // ---- 1. Renders with no props ----
  test('renders with no props — shows "No Git" on left side, no right section', async () => {
    await act(async () => { root.render(<StatusBar />); });

    expect(container.querySelector('[data-testid="git-branch-icon"]')).toBeTruthy();
    expect(getByText(container, 'No Git')).toBeTruthy();

    // Right section should not be present
    expect(container.querySelector('.statusbar-right')).toBeNull();
  });

  // ---- 2. Shows git branch name ----
  test('shows git branch name when branch prop is provided', async () => {
    await act(async () => { root.render(<StatusBar branch="feature/my-branch" />); });

    expect(getByText(container, 'feature/my-branch')).toBeTruthy();
    expect(queryByText(container, 'No Git')).toBeNull();
  });

  // ---- 3. Semantic HTML ----
  test('renders as a <footer> with aria-label for landmark navigation', async () => {
    await act(async () => { root.render(<StatusBar />); });

    const footer = container.querySelector('footer.statusbar');
    expect(footer).toBeTruthy();
    expect(footer?.getAttribute('aria-label')).toBe('Editor status bar');
    // Must NOT have role="status" (implicit aria-live="polite") — would spam
    // screen readers on every cursor-position change while typing.
    expect(footer?.hasAttribute('role')).toBe(false);
    expect(footer?.hasAttribute('aria-live')).toBe(false);
  });

  // ---- 4. Shows right section when buffer is provided ----
  test('shows right section with cursor, language, encoding, line endings, indentation when buffer is provided', async () => {
    await act(async () => { root.render(<StatusBar branch="main" buffer={makeBuffer()} />); });

    const right = container.querySelector('.statusbar-right');
    expect(right).toBeTruthy();

    // Cursor
    expect(getByText(container, 'Ln 1, Col 1')).toBeTruthy();
    // Language
    expect(getByText(container, 'TypeScript (JSX)')).toBeTruthy();
    // Encoding
    expect(getByText(container, 'UTF-8')).toBeTruthy();
    // Line endings
    expect(getByText(container, 'LF')).toBeTruthy();
    // Indentation (hardcoded default)
    expect(getByText(container, 'Spaces: 2')).toBeTruthy();
  });

  // ---- 5. Cursor position display (1-based from 0-based) ----
  test('displays cursor position as 1-based values', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={makeBuffer({
            cursorPosition: { line: 4, column: 9 },
          })}
        />,
      );
    });

    expect(getByText(container, 'Ln 5, Col 10')).toBeTruthy();
  });

  // ---- 6. Language detection from file extension ----
  test('detects language from file extension', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={makeBuffer({
            file: { name: 'app.ts', ext: '.ts' },
          })}
        />,
      );
    });

    expect(getByText(container, 'TypeScript')).toBeTruthy();
  });

  // ---- 7. Language detection with override ----
  test('uses language override when provided, ignoring file extension', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={makeBuffer({
            file: { name: 'app.ts', ext: '.ts' },
            languageOverride: 'go',
          })}
        />,
      );
    });

    expect(getByText(container, 'Go')).toBeTruthy();
    expect(queryByText(container, 'TypeScript')).toBeNull();
  });

  // ---- 8b. Non-file buffer kind ----
  test('shows capitalized kind name for non-file buffers', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={{
            kind: 'chat',
            content: '',
          }}
        />,
      );
    });

    expect(getByText(container, 'Chat')).toBeTruthy();
  });

  test('shows capitalized kind name for diff kind', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={{
            kind: 'diff',
            content: '',
          }}
        />,
      );
    });

    expect(getByText(container, 'Diff')).toBeTruthy();
  });

  // ---- 9. Line endings detection ----
  test('shows "LF" when content contains no CRLF', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={makeBuffer({ content: 'line1\nline2\nline3' })}
        />,
      );
    });

    expect(getByText(container, 'LF')).toBeTruthy();
  });

  test('shows "CRLF" when content contains only \\r\\n', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={makeBuffer({ content: 'line1\r\nline2\r\nline3' })}
        />,
      );
    });

    // After removing all \r\n sequences, no bare \n remains → hasBareLF=false → "CRLF".
    expect(getByText(container, 'CRLF')).toBeTruthy();
  });

  test('shows "Mixed" when content has both CRLF and bare LF', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={makeBuffer({ content: 'line1\nline2\r\nline3' })}
        />,
      );
    });

    // After removing \r\n, bare \n remains → hasBareLF=true + hasCRLF=true → "Mixed".
    expect(getByText(container, 'Mixed')).toBeTruthy();
  });

  // ---- 10. No buffer ----
  test('does not render right section when buffer is null', async () => {
    await act(async () => { root.render(<StatusBar branch="main" buffer={null} />); });

    expect(container.querySelector('.statusbar-right')).toBeNull();
  });

  // ---- 11. Buffer without cursorPosition ----
  test('does not show cursor position when buffer has no cursorPosition', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={makeBuffer({
            cursorPosition: undefined,
          })}
        />,
      );
    });

    expect(container.querySelector('.statusbar-right')).toBeTruthy();
    expect(
      container.querySelector('.statusbar-item-cursor'),
    ).toBeNull();
  });

  test('does not show cursor position when cursorPosition has non-numeric values', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={makeBuffer({
            cursorPosition: { line: undefined as any, column: undefined as any },
          })}
        />,
      );
    });

    expect(
      container.querySelector('.statusbar-item-cursor'),
    ).toBeNull();
  });

  // ---- 12. Plain text for unknown extensions ----
  test('shows "Plain Text" for unknown file extensions', async () => {
    await act(async () => {
      root.render(
        <StatusBar
          buffer={makeBuffer({
            file: { name: 'data.xyz', ext: '.xyz' },
          })}
        />,
      );
    });

    expect(getByText(container, 'Plain Text')).toBeTruthy();
  });

  // ---- 13. Title attributes for accessibility ----
  test('items have title attributes for accessibility', async () => {
    await act(async () => { root.render(<StatusBar branch="main" buffer={makeBuffer()} />); });

    expect(container.querySelector('[title="Branch: main"]')).toBeTruthy();
    expect(container.querySelector('[title="Cursor position"]')).toBeTruthy();
    expect(container.querySelector('[title="Language: TypeScript (JSX)"]')).toBeTruthy();
    expect(container.querySelector('[title="File encoding"]')).toBeTruthy();
    expect(container.querySelector('[title="Line ending format"]')).toBeTruthy();
    expect(container.querySelector('[title="Indentation"]')).toBeTruthy();
  });

  // ---- 14. Cursor position is aria-hidden ----
  test('cursor position span has aria-hidden to avoid screen reader spam', async () => {
    await act(async () => { root.render(<StatusBar buffer={makeBuffer()} />); });

    const cursorEl = document.querySelector('.statusbar-item-cursor');
    expect(cursorEl).toBeTruthy();
    expect(cursorEl?.getAttribute('aria-hidden')).toBe('true');
  });

  // ---- 15. Empty branch string shows "No Git" ----
  test('empty string branch shows "No Git"', async () => {
    await act(async () => { root.render(<StatusBar branch="" />); });

    expect(getByText(container, 'No Git')).toBeTruthy();
  });

  // ---- 16. Custom encoding when provided ----
  test('shows custom encoding when encoding prop is provided', async () => {
    await act(async () => { root.render(<StatusBar buffer={makeBuffer()} encoding="ISO-8859-1" />); });

    expect(getByText(container, 'ISO-8859-1')).toBeTruthy();
  });

  // ---- 17. Custom indentation when provided ----
  test('shows custom indentation when indentation prop is provided', async () => {
    await act(async () => { root.render(<StatusBar buffer={makeBuffer()} indentation="Tabs: 4" />); });

    expect(getByText(container, 'Tabs: 4')).toBeTruthy();
  });
});
