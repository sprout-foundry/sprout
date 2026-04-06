import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
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
    file: { name: 'app.tsx', ext: 'tsx' },
    content: 'hello world\n',
    cursorPosition: { line: 0, column: 0 },
    languageOverride: null,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('StatusBar', () => {
  // ---- 1. Renders with no props ----
  test('renders with no props — shows "No Git" on left side, no right section', () => {
    const { container } = render(<StatusBar />);

    expect(screen.getByTestId('git-branch-icon')).toBeInTheDocument();
    expect(screen.getByText('No Git')).toBeInTheDocument();

    // Right section should not be present
    expect(container.querySelector('.statusbar-right')).not.toBeInTheDocument();
  });

  // ---- 2. Shows git branch name ----
  test('shows git branch name when branch prop is provided', () => {
    render(<StatusBar branch="feature/my-branch" />);

    expect(screen.getByText('feature/my-branch')).toBeInTheDocument();
    expect(screen.queryByText('No Git')).not.toBeInTheDocument();
  });

  // ---- 3. Semantic HTML ----
  test('renders as a <footer> with aria-label for landmark navigation', () => {
    const { container } = render(<StatusBar />);

    const footer = container.querySelector('footer.statusbar');
    expect(footer).toBeInTheDocument();
    expect(footer).toHaveAttribute('aria-label', 'Editor status bar');
    // Must NOT have role="status" (implicit aria-live="polite") — would spam
    // screen readers on every cursor-position change while typing.
    expect(footer).not.toHaveAttribute('role');
    expect(footer).not.toHaveAttribute('aria-live');
  });

  // ---- 4. Shows right section when buffer is provided ----
  test('shows right section with cursor, language, encoding, line endings, indentation when buffer is provided', () => {
    const { container } = render(
      <StatusBar branch="main" buffer={makeBuffer()} />,
    );

    const right = container.querySelector('.statusbar-right');
    expect(right).toBeInTheDocument();

    // Cursor
    expect(screen.getByText('Ln 1, Col 1')).toBeInTheDocument();
    // Language
    expect(screen.getByText('TypeScript (JSX)')).toBeInTheDocument();
    // Encoding
    expect(screen.getByText('UTF-8')).toBeInTheDocument();
    // Line endings
    expect(screen.getByText('LF')).toBeInTheDocument();
    // Indentation (hardcoded default)
    expect(screen.getByText('Spaces: 2')).toBeInTheDocument();
  });

  // ---- 5. Cursor position display (1-based from 0-based) ----
  test('displays cursor position as 1-based values', () => {
    render(
      <StatusBar
        buffer={makeBuffer({
          cursorPosition: { line: 4, column: 9 },
        })}
      />,
    );

    expect(screen.getByText('Ln 5, Col 10')).toBeInTheDocument();
  });

  // ---- 6. Language detection from file extension ----
  test('detects language from file extension', () => {
    render(
      <StatusBar
        buffer={makeBuffer({
          file: { name: 'app.ts', ext: 'ts' },
        })}
      />,
    );

    expect(screen.getByText('TypeScript')).toBeInTheDocument();
  });

  // ---- 7. Language detection with override ----
  test('uses language override when provided, ignoring file extension', () => {
    render(
      <StatusBar
        buffer={makeBuffer({
          file: { name: 'app.ts', ext: 'ts' },
          languageOverride: 'go',
        })}
      />,
    );

    expect(screen.getByText('Go')).toBeInTheDocument();
    expect(screen.queryByText('TypeScript')).not.toBeInTheDocument();
  });

  // ---- 8b. Non-file buffer kind ----
  test('shows capitalized kind name for non-file buffers', () => {
    render(
      <StatusBar
        buffer={{
          kind: 'chat',
          content: '',
        }}
      />,
    );

    expect(screen.getByText('Chat')).toBeInTheDocument();
  });

  test('shows capitalized kind name for diff kind', () => {
    render(
      <StatusBar
        buffer={{
          kind: 'diff',
          content: '',
        }}
      />,
    );

    expect(screen.getByText('Diff')).toBeInTheDocument();
  });

  // ---- 9. Line endings detection ----
  test('shows "LF" when content contains no CRLF', () => {
    render(
      <StatusBar
        buffer={makeBuffer({ content: 'line1\nline2\nline3' })}
      />,
    );

    expect(screen.getByText('LF')).toBeInTheDocument();
  });

  test('shows "CRLF" when content contains only \\r\\n', () => {
    render(
      <StatusBar
        buffer={makeBuffer({ content: 'line1\r\nline2\r\nline3' })}
      />,
    );

    // After removing all \r\n sequences, no bare \n remains → hasBareLF=false → "CRLF".
    expect(screen.getByText('CRLF')).toBeInTheDocument();
  });

  test('shows "Mixed" when content has both CRLF and bare LF', () => {
    render(
      <StatusBar
        buffer={makeBuffer({ content: 'line1\nline2\r\nline3' })}
      />,
    );

    // After removing \r\n, bare \n remains → hasBareLF=true + hasCRLF=true → "Mixed".
    expect(screen.getByText('Mixed')).toBeInTheDocument();
  });

  // ---- 10. No buffer ----
  test('does not render right section when buffer is null', () => {
    const { container } = render(<StatusBar branch="main" buffer={null} />);

    expect(container.querySelector('.statusbar-right')).not.toBeInTheDocument();
  });

  // ---- 11. Buffer without cursorPosition ----
  test('does not show cursor position when buffer has no cursorPosition', () => {
    const { container } = render(
      <StatusBar
        buffer={makeBuffer({
          cursorPosition: undefined,
        })}
      />,
    );

    expect(container.querySelector('.statusbar-right')).toBeInTheDocument();
    expect(
      container.querySelector('.statusbar-item-cursor'),
    ).not.toBeInTheDocument();
  });

  test('does not show cursor position when cursorPosition has non-numeric values', () => {
    const { container } = render(
      <StatusBar
        buffer={makeBuffer({
          cursorPosition: { line: undefined as any, column: undefined as any },
        })}
      />,
    );

    expect(
      container.querySelector('.statusbar-item-cursor'),
    ).not.toBeInTheDocument();
  });

  // ---- 12. Plain text for unknown extensions ----
  test('shows "Plain Text" for unknown file extensions', () => {
    render(
      <StatusBar
        buffer={makeBuffer({
          file: { name: 'data.xyz', ext: 'xyz' },
        })}
      />,
    );

    expect(screen.getByText('Plain Text')).toBeInTheDocument();
  });

  // ---- 13. Title attributes for accessibility ----
  test('items have title attributes for accessibility', () => {
    render(<StatusBar branch="main" buffer={makeBuffer()} />);

    expect(screen.getByTitle('Branch: main')).toBeInTheDocument();
    expect(screen.getByTitle('Cursor position')).toBeInTheDocument();
    expect(screen.getByTitle('Language: TypeScript (JSX)')).toBeInTheDocument();
    expect(screen.getByTitle('File encoding')).toBeInTheDocument();
    expect(screen.getByTitle('Line ending format')).toBeInTheDocument();
    expect(screen.getByTitle('Indentation')).toBeInTheDocument();
  });

  // ---- 14. Cursor position is aria-hidden ----
  test('cursor position span has aria-hidden to avoid screen reader spam', () => {
    render(<StatusBar buffer={makeBuffer()} />);

    const cursorEl = document.querySelector('.statusbar-item-cursor');
    expect(cursorEl).toBeInTheDocument();
    expect(cursorEl).toHaveAttribute('aria-hidden', 'true');
  });

  // ---- 15. Empty branch string shows "No Git" ----
  test('empty string branch shows "No Git"', () => {
    render(<StatusBar branch="" />);

    expect(screen.getByText('No Git')).toBeInTheDocument();
  });
});
