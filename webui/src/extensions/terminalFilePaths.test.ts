import type { Terminal, IBuffer, IBufferLine, IDisposable } from '@xterm/xterm';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { filePathPattern, parseFilePathMatch, registerTerminalFilePathLinks } from './terminalFilePaths';

// ============================================================================
// Helpers
// ============================================================================

/** Reset the global regex lastIndex before each test */
function resetRegex() {
  filePathPattern.lastIndex = 0;
}

/** Execute regex against text and return all matches */
function getAllMatches(text: string): RegExpExecArray[] {
  resetRegex();
  const results: RegExpExecArray[] = [];
  let match: RegExpExecArray | null;
  while ((match = filePathPattern.exec(text)) !== null) {
    results.push(match);
  }
  return results;
}

/** Build a minimal mock Terminal for testing registerTerminalFilePathLinks */
function createMockTerminal(lines: string[]): Terminal {
  const getLine = vi.fn((index: number): IBufferLine | null => {
    const line = lines[index];
    if (line === undefined) return null;
    return {
      length: line.length,
      getCell: vi.fn(() => null),
      getString: vi.fn(() => line),
      translateToString: vi.fn((trim: boolean) => {
        if (trim) return line.replace(/\s+$/, '');
        return line;
      }),
      isWrapped: false,
      attrs: 0,
      fg: 0,
      bg: 0,
      combinedData: '',
    };
  });

  const mockBuffer = {
    active: { getLine },
    normal: { getLine },
  } as unknown as IBuffer;

  const registerLinkProvider = vi.fn((provider: unknown) => {
    return {
      dispose: vi.fn(),
    } as unknown as IDisposable;
  });

  return {
    buffer: mockBuffer,
    registerLinkProvider,
  } as unknown as Terminal;
}

/**
 * Invoke the registered provider's provideLinks and return the resolved links.
 * provideLinks calls its callback synchronously, so we capture the result directly.
 */
function getLinksFromProvider(terminal: Terminal, bufferLineNumber: number): unknown {
  const registeredProvider = (terminal.registerLinkProvider as ReturnType<typeof vi.fn>).mock.calls[0][0];
  let result: unknown;
  registeredProvider.provideLinks(bufferLineNumber, (links: unknown) => {
    result = links;
  });
  return result;
}

// ============================================================================
// filePathPattern
// ============================================================================

describe('filePathPattern', () => {
  describe('positive matches', () => {
    it('matches ./foo.go:12:34', () => {
      const matches = getAllMatches('./foo.go:12:34');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./foo.go:12:34');
    });

    it('matches ./foo.go:12 (no column)', () => {
      const matches = getAllMatches('./foo.go:12');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./foo.go:12');
    });

    it('matches foo.go:12:34 (no leading ./ or /)', () => {
      const matches = getAllMatches('foo.go:12:34');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('foo.go:12:34');
    });

    it('matches foo.go:12 (no leading ./ or /, no column)', () => {
      const matches = getAllMatches('foo.go:12');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('foo.go:12');
    });

    it('matches /abs/path/to/file.ts:5:1', () => {
      const matches = getAllMatches('/abs/path/to/file.ts:5:1');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('/abs/path/to/file.ts:5:1');
    });

    it('matches /abs/path/to/file.ts:5 (no column)', () => {
      const matches = getAllMatches('/abs/path/to/file.ts:5');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('/abs/path/to/file.ts:5');
    });

    it('matches path in error message with trailing text', () => {
      const matches = getAllMatches('error at ./main.go:42:10 undefined');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./main.go:42:10');
    });

    it('matches path in parenthesized context: (./server.go:55:3)', () => {
      const matches = getAllMatches('(./server.go:55:3)');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./server.go:55:3');
    });

    it('matches path with dots in directory names', () => {
      const matches = getAllMatches('./src/v2.0/module.ts:10:20');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./src/v2.0/module.ts:10:20');
    });

    it('matches path with hyphens in filename', () => {
      const matches = getAllMatches('./my-file-name.tsx:1');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./my-file-name.tsx:1');
    });

    it('matches multiple paths on same line', () => {
      const matches = getAllMatches('./a.go:1 ./b.go:2');
      expect(matches).toHaveLength(2);
      expect(matches[0][0]).toBe('./a.go:1');
      expect(matches[1][0]).toBe('./b.go:2');
    });

    it('matches path at start of line', () => {
      const matches = getAllMatches('main.go:42 some text');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('main.go:42');
    });

    it('matches path at end of line', () => {
      const matches = getAllMatches('some text ./file.go:5');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./file.go:5');
    });

    it('matches path before semicolon', () => {
      const matches = getAllMatches('error: ./file.go:10; more');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./file.go:10');
    });

    it('matches path before closing paren', () => {
      const matches = getAllMatches('see ./file.go:10)');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./file.go:10');
    });

    it('matches path before comma', () => {
      const matches = getAllMatches('see ./file.go:10, then');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./file.go:10');
    });

    it('matches path at end of string (no trailing delimiter needed)', () => {
      const matches = getAllMatches('error at ./main.go:42:10');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./main.go:42:10');
    });

    it('matches Go compiler error format: ./foo.go:10:5: undefined', () => {
      const matches = getAllMatches('error: ./foo.go:10:5: undefined');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('./foo.go:10:5');
    });

    it('matches path followed by colon-space (compiler output)', () => {
      const matches = getAllMatches('warn: bar.ts:20: something');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('bar.ts:20');
    });
  });

  describe('negative cases', () => {
    it('does not match URL: http://host:80', () => {
      const matches = getAllMatches('http://host:80');
      expect(matches).toHaveLength(0);
    });

    it('does not match URL: https://example.com:443/path', () => {
      const matches = getAllMatches('https://example.com:443/path');
      expect(matches).toHaveLength(0);
    });

    it('does not match bare filename without line number: foo.go', () => {
      const matches = getAllMatches('foo.go');
      expect(matches).toHaveLength(0);
    });

    it('does not match bare filename with ./ prefix: ./foo.go', () => {
      const matches = getAllMatches('./foo.go');
      expect(matches).toHaveLength(0);
    });

    it('does not match plain text without dots', () => {
      const matches = getAllMatches('just plain text');
      expect(matches).toHaveLength(0);
    });

    it('does not match version strings like v1.2.3', () => {
      const matches = getAllMatches('v1.2.3');
      expect(matches).toHaveLength(0);
    });

    it('does not match time formats like 12:34:56', () => {
      const matches = getAllMatches('time 12:34:56');
      expect(matches).toHaveLength(0);
    });

    it('does not match arbitrary text with dots but no line number', () => {
      const matches = getAllMatches('some.random.stuff');
      expect(matches).toHaveLength(0);
    });

    it('does not match IP addresses: 192.168.1.1:8080', () => {
      const matches = getAllMatches('Connecting to 192.168.1.1:8080');
      expect(matches).toHaveLength(0);
    });

    it('does not match IP addresses: 10.0.0.1:3000', () => {
      const matches = getAllMatches('server at 10.0.0.1:3000 failed');
      expect(matches).toHaveLength(0);
    });

    it('does not match empty string', () => {
      const matches = getAllMatches('');
      expect(matches).toHaveLength(0);
    });

    it('does not match whitespace-only string', () => {
      const matches = getAllMatches('   ');
      expect(matches).toHaveLength(0);
    });

    it('matches valid portion after space in ./my file.go:10', () => {
      // Space is a boundary — regex matches "file.go:10" (the valid part after space)
      const matches = getAllMatches('./my file.go:10');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('file.go:10');
    });

    it('matches valid portion after space in directory: ./my dir/file.go:10', () => {
      // Space causes regex to see "dir/file.go:10" as valid subpath match
      const matches = getAllMatches('./my dir/file.go:10');
      expect(matches).toHaveLength(1);
      expect(matches[0][0]).toBe('dir/file.go:10');
    });
  });
});

// ============================================================================
// parseFilePathMatch
// ============================================================================

describe('parseFilePathMatch', () => {
  it('extracts path, line, and column from ./foo.go:12:34', () => {
    resetRegex();
    const match = filePathPattern.exec('./foo.go:12:34');
    expect(match).not.toBeNull();
    const result = parseFilePathMatch(match!);
    expect(result.path).toBe('foo.go');
    expect(result.lineNumber).toBe(12);
    expect(result.columnNumber).toBe(34);
  });

  it('extracts path and line without column from ./foo.go:12', () => {
    resetRegex();
    const match = filePathPattern.exec('./foo.go:12');
    expect(match).not.toBeNull();
    const result = parseFilePathMatch(match!);
    expect(result.path).toBe('foo.go');
    expect(result.lineNumber).toBe(12);
    expect(result.columnNumber).toBeUndefined();
  });

  it('strips leading ./ from path', () => {
    resetRegex();
    const match = filePathPattern.exec('./main.go:42:10');
    expect(match).not.toBeNull();
    const result = parseFilePathMatch(match!);
    expect(result.path).toBe('main.go');
    expect(result.lineNumber).toBe(42);
    expect(result.columnNumber).toBe(10);
  });

  it('preserves absolute paths without stripping', () => {
    resetRegex();
    const match = filePathPattern.exec('/abs/path/to/file.ts:5:1');
    expect(match).not.toBeNull();
    const result = parseFilePathMatch(match!);
    expect(result.path).toBe('/abs/path/to/file.ts');
    expect(result.lineNumber).toBe(5);
    expect(result.columnNumber).toBe(1);
  });

  it('preserves bare filenames without ./ or /', () => {
    resetRegex();
    const match = filePathPattern.exec('foo.go:12:34');
    expect(match).not.toBeNull();
    const result = parseFilePathMatch(match!);
    expect(result.path).toBe('foo.go');
    expect(result.lineNumber).toBe(12);
    expect(result.columnNumber).toBe(34);
  });

  it('handles paths with directory segments', () => {
    resetRegex();
    const match = filePathPattern.exec('./src/v2.0/module.ts:10:20');
    expect(match).not.toBeNull();
    const result = parseFilePathMatch(match!);
    expect(result.path).toBe('src/v2.0/module.ts');
    expect(result.lineNumber).toBe(10);
    expect(result.columnNumber).toBe(20);
  });

  it('handles large line numbers', () => {
    resetRegex();
    const match = filePathPattern.exec('./big.go:99999');
    expect(match).not.toBeNull();
    const result = parseFilePathMatch(match!);
    expect(result.path).toBe('big.go');
    expect(result.lineNumber).toBe(99999);
  });
});

// ============================================================================
// registerTerminalFilePathLinks
// ============================================================================

describe('registerTerminalFilePathLinks', () => {
  const eventHandler: ((e: Event) => void) & { collected: CustomEvent[] } = Object.assign(
    (e: Event) => {
      eventHandler.collected.push(e as CustomEvent);
    },
    { collected: [] },
  );

  beforeEach(() => {
    eventHandler.collected = [];
    window.addEventListener('sprout:open-in-editor', eventHandler);
  });

  afterEach(() => {
    window.removeEventListener('sprout:open-in-editor', eventHandler);
  });

  function getDispatchedEvents(): CustomEvent[] {
    return eventHandler.collected;
  }

  it('registers a link provider on the terminal', () => {
    const terminal = createMockTerminal(['./foo.go:12:34']);
    const disposable = registerTerminalFilePathLinks(terminal);

    expect(terminal.registerLinkProvider).toHaveBeenCalledTimes(1);
    expect(disposable).toBeDefined();
    expect(typeof disposable.dispose).toBe('function');
  });

  it('returns an IDisposable with dispose method', () => {
    const terminal = createMockTerminal([]);
    const disposable = registerTerminalFilePathLinks(terminal);
    expect(typeof disposable.dispose).toBe('function');
    disposable.dispose(); // should not throw
  });

  it('scans line and produces links with correct range and text', () => {
    const terminal = createMockTerminal(['  ./foo.go:12:34  ']);
    registerTerminalFilePathLinks(terminal);

    const links = getLinksFromProvider(terminal, 1) as Array<{
      range: { start: { x: number; y: number }; end: { x: number; y: number } };
      text: string;
      activate: (event: MouseEvent, text: string) => void;
      decorations: { underline: boolean; pointerCursor: boolean };
    }>;

    expect(Array.isArray(links)).toBe(true);
    expect(links).toHaveLength(1);

    const link = links[0];
    expect(link.text).toBe('./foo.go:12:34');
    // Match starts at index 2 (after two leading spaces), x is 1-based in xterm
    expect(link.range.start.x).toBe(3);
    expect(link.range.start.y).toBe(1);
    expect(link.range.end.x).toBe(3 + './foo.go:12:34'.length);
    expect(link.range.end.y).toBe(1);

    // Check decorations
    expect(link.decorations.underline).toBe(true);
    expect(link.decorations.pointerCursor).toBe(true);
  });

  it('activate dispatches sprout:open-in-editor event with correct detail', () => {
    const terminal = createMockTerminal(['./main.go:42:10']);
    registerTerminalFilePathLinks(terminal);

    const links = getLinksFromProvider(terminal, 1) as Array<{
      activate: (event: MouseEvent, text: string) => void;
    }>;

    links[0].activate(new MouseEvent('click'), './main.go:42:10');

    expect(getDispatchedEvents()).toHaveLength(1);
    const evt = getDispatchedEvents()[0];
    expect(evt.type).toBe('sprout:open-in-editor');
    expect(evt.detail).toEqual({ path: 'main.go', lineNumber: 42, columnNumber: 10 });
  });

  it('strips leading ./ from path in event detail', () => {
    const terminal = createMockTerminal(['./foo.go:5']);
    registerTerminalFilePathLinks(terminal);

    const links = getLinksFromProvider(terminal, 1) as Array<{
      activate: (event: MouseEvent, text: string) => void;
    }>;

    links[0].activate(new MouseEvent('click'), './foo.go:5');

    expect(getDispatchedEvents()).toHaveLength(1);
    expect(getDispatchedEvents()[0].detail).toEqual({ path: 'foo.go', lineNumber: 5 });
  });

  it('includes columnNumber in event detail when present', () => {
    const terminal = createMockTerminal(['./file.ts:10:20']);
    registerTerminalFilePathLinks(terminal);

    const links = getLinksFromProvider(terminal, 1) as Array<{
      activate: (event: MouseEvent, text: string) => void;
    }>;

    links[0].activate(new MouseEvent('click'), './file.ts:10:20');

    expect(getDispatchedEvents()[0].detail).toEqual({
      path: 'file.ts',
      lineNumber: 10,
      columnNumber: 20,
    });
  });

  it('includes columnNumber as undefined in event detail when no column in match', () => {
    const terminal = createMockTerminal(['./file.ts:10']);
    registerTerminalFilePathLinks(terminal);

    const links = getLinksFromProvider(terminal, 1) as Array<{
      activate: (event: MouseEvent, text: string) => void;
    }>;

    links[0].activate(new MouseEvent('click'), './file.ts:10');

    expect(getDispatchedEvents()[0].detail).toEqual({
      path: 'file.ts',
      lineNumber: 10,
      columnNumber: undefined,
    });
  });

  it('handles line with no file paths (returns undefined)', () => {
    const terminal = createMockTerminal(['just some plain text']);
    registerTerminalFilePathLinks(terminal);

    const links = getLinksFromProvider(terminal, 1);
    expect(links).toBeUndefined();
  });

  it('handles missing line (null from getLine)', () => {
    const terminal = createMockTerminal([]);
    registerTerminalFilePathLinks(terminal);

    const links = getLinksFromProvider(terminal, 99);
    expect(links).toBeUndefined();
  });

  it('handles empty line text', () => {
    const terminal = createMockTerminal(['']);
    registerTerminalFilePathLinks(terminal);

    const links = getLinksFromProvider(terminal, 1);
    expect(links).toBeUndefined();
  });

  it('handles multiple file paths on the same line', () => {
    const terminal = createMockTerminal(['./a.go:1 ./b.go:2']);
    registerTerminalFilePathLinks(terminal);

    const links = getLinksFromProvider(terminal, 1) as Array<{
      text: string;
      range: { start: { x: number; y: number }; end: { x: number; y: number } };
    }>;

    expect(links).toHaveLength(2);
    expect(links[0].text).toBe('./a.go:1');
    expect(links[1].text).toBe('./b.go:2');
  });

  it('uses correct buffer line number in range y', () => {
    const terminal = createMockTerminal(['first line', './file.go:42', 'third line']);
    registerTerminalFilePathLinks(terminal);

    const links = getLinksFromProvider(terminal, 2) as Array<{
      range: { start: { y: number }; end: { y: number } };
    }>;

    expect(links[0].range.start.y).toBe(2);
    expect(links[0].range.end.y).toBe(2);
  });

  it('calls getLine with 0-based index derived from 1-based bufferLineNumber', () => {
    const terminal = createMockTerminal(['./a.ts:1', './b.ts:2']);
    registerTerminalFilePathLinks(terminal);

    // Request line 2 (1-based) -> should call getLine(1) (0-based)
    getLinksFromProvider(terminal, 2);

    const getLine = terminal.buffer.active.getLine as ReturnType<typeof vi.fn>;
    expect(getLine).toHaveBeenCalledWith(1);
  });
});
