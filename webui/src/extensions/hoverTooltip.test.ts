/**
 * hoverTooltip.test.ts — Unit tests for the hoverTooltip extension.
 *
 * Tests escapeHtml, formatMarkdown, and createHoverTooltipExtension.
 * Uses Jest mocking patterns consistent with other test files in the project.
 */

// ── Mock modules before imports ───────────────────────────────────────

jest.mock('@codemirror/view', () => ({
  hoverTooltip: jest.fn((source, options) => ({ type: 'Extension', source, options })),
  EditorView: {
    theme: jest.fn(() => []),
  },
  keymap: {
    of: jest.fn(() => []),
  },
}));

jest.mock('../services/api', () => ({
  ApiService: {
    getInstance: jest.fn(() => ({
      getSemanticHover: jest.fn(),
    })),
  },
}));

jest.mock('./languageRegistry', () => ({
  resolveLanguageId: jest.fn(),
}));

jest.mock('../utils/log', () => ({
  debugLog: jest.fn(),
}));

// Mock LSPClientService — it imports @codemirror/lsp-client (ESM-only)
jest.mock('../services/lspClientService', () => ({
  LSPClientService: class {
    static get lspClientService() {
      return { isSupported: jest.fn(() => false) };
    }
  },
}));

// ── Module under test ─────────────────────────────────────────────────

import { escapeHtml, formatMarkdown, createHoverTooltipExtension } from './hoverTooltip';

// ── escapeHtml tests ────────────────────────────────────────────────────

describe('escapeHtml', () => {
  // -------------------------------------------------------------------------
  // Basic escaping
  // -------------------------------------------------------------------------

  it('escapes ampersands', () => {
    expect(escapeHtml('a & b')).toBe('a &amp; b');
  });

  it('escapes angle brackets - less than', () => {
    expect(escapeHtml('a < b')).toBe('a &lt; b');
  });

  it('escapes angle brackets - greater than', () => {
    expect(escapeHtml('a > b')).toBe('a &gt; b');
  });

  it('escapes ampersand before brackets (order matters)', () => {
    // & should be escaped first, then < and >
    expect(escapeHtml('a & <b>')).toBe('a &amp; &lt;b&gt;');
  });

  // -------------------------------------------------------------------------
  // Empty / plain text
  // -------------------------------------------------------------------------

  it('returns empty string unchanged', () => {
    expect(escapeHtml('')).toBe('');
  });

  it('returns plain text unchanged', () => {
    expect(escapeHtml('hello world')).toBe('hello world');
  });

  // -------------------------------------------------------------------------
  // Mixed content
  // -------------------------------------------------------------------------

  it('handles mixed content', () => {
    expect(escapeHtml('func(a: number): string')).toBe('func(a: number): string');
  });

  it('handles multiple occurrences', () => {
    expect(escapeHtml('<a> and <b> and <c>')).toBe('&lt;a&gt; and &lt;b&gt; and &lt;c&gt;');
  });

  it('does NOT escape quotes (only &lt; &gt; &amp;)', () => {
    // Quotes should remain as-is
    expect(escapeHtml('"hello"')).toBe('"hello"');
    expect(escapeHtml("'hello'")).toBe("'hello'");
  });
});

// ── formatMarkdown tests ────────────────────────────────────────────────

describe('formatMarkdown', () => {
  // -------------------------------------------------------------------------
  // Empty / plain text
  // -------------------------------------------------------------------------

  it('returns empty string unchanged', () => {
    expect(formatMarkdown('')).toBe('');
  });

  it('returns plain text unchanged', () => {
    expect(formatMarkdown('hello world')).toBe('hello world');
  });

  // -------------------------------------------------------------------------
  // HTML escaping
  // -------------------------------------------------------------------------

  it('escapes HTML first (e.g., <script> becomes &lt;script&gt;)', () => {
    expect(formatMarkdown('<script>alert(1)</script>')).toBe('&lt;script&gt;alert(1)&lt;/script&gt;');
  });

  it('handles XSS prevention (script tags in content)', () => {
    // The script tag should be escaped, not executed
    expect(formatMarkdown('<img src=x onerror=alert(1)>')).toBe('&lt;img src=x onerror=alert(1)&gt;');
  });

  // -------------------------------------------------------------------------
  // Code formatting
  // -------------------------------------------------------------------------

  it('converts code blocks (```...```) to <pre><code>...</code></pre>', () => {
    const input = '```\nconst x = 1;\n```';
    const expected = '<pre><code>const x = 1;</code></pre>';
    expect(formatMarkdown(input)).toBe(expected);
  });

  it('converts code blocks with language specifier', () => {
    const input = '```js\nconst x = 1;\n```';
    const expected = '<pre><code>const x = 1;</code></pre>';
    expect(formatMarkdown(input)).toBe(expected);
  });

  it('converts inline code (backticks) to <code>...</code>', () => {
    expect(formatMarkdown('Use `console.log()` for debugging')).toBe(
      'Use <code>console.log()</code> for debugging'
    );
  });

  // -------------------------------------------------------------------------
  // Text formatting
  // -------------------------------------------------------------------------

  it('converts **bold** to <strong>...</strong>', () => {
    expect(formatMarkdown('This is **bold** text')).toBe('This is <strong>bold</strong> text');
  });

  it('convert *italic* to <em>...</em>', () => {
    expect(formatMarkdown('This is *italic* text')).toBe('This is <em>italic</em> text');
    // Note: formatMarkdown uses `*text*` which becomes <em>text</em>
  });

  it('converts newlines to <br>', () => {
    expect(formatMarkdown('line1\nline2')).toBe('line1<br>line2');
  });

  // -------------------------------------------------------------------------
  // Mixed markdown
  // -------------------------------------------------------------------------

  it('handles mixed markdown', () => {
    const input = '**Bold** and *italic* and `code`';
    const expected = '<strong>Bold</strong> and <em>italic</em> and <code>code</code>';
    expect(formatMarkdown(input)).toBe(expected);
  });

  it('handles code block with bold inside (conversion order matters)', () => {
    // Note: Bold IS converted inside code blocks because escapeHtml already ran
    // and then the code block regex runs.
    const input = '```js\nconst **x** = 1;\n```';
    const expected = '<pre><code>const <strong>x</strong> = 1;</code></pre>';
    expect(formatMarkdown(input)).toBe(expected);
  });
});

// ── createHoverTooltipExtension tests ──────────────────────────────────────

describe('createHoverTooltipExtension', () => {
  // Import mocks
  const mockHoverTooltip = require('@codemirror/view').hoverTooltip;
  const MockApiService = require('../services/api').ApiService;
  const mockResolveLanguageId = require('./languageRegistry').resolveLanguageId;
  const mockDebugLog = require('../utils/log').debugLog;

  beforeEach(() => {
    jest.clearAllMocks();
  });

  // -------------------------------------------------------------------------
  // Basic tests
  // -------------------------------------------------------------------------

  it('returns an extension (or undefined if mocked keymap returns empty array)', () => {
    const extension = createHoverTooltipExtension(
      () => undefined,
      () => ''
    );
    // Note: With mocked keymap.of returning [], this may be an empty array or undefined.
    // The important thing is that it doesn't throw.
    expect(extension === undefined || Array.isArray(extension)).toBe(true);
  });

  it('returns an extension with hoverTooltip called', () => {
    const extension = createHoverTooltipExtension(
      () => undefined,
      () => ''
    );
    expect(mockHoverTooltip).toHaveBeenCalled();
  });

  // -------------------------------------------------------------------------
  // File path validation
  // -------------------------------------------------------------------------

  it('hover source returns null for no file path', async () => {
    const getFilePath = () => undefined;
    const getContent = () => 'const x = 1;';

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    // Create a mock view
    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 10 })),
        },
      },
    };

    const result = await source(mockView, 5);
    expect(result).toBeNull();
  });

  it('hover source returns null for __workspace/ paths', async () => {
    const getFilePath = () => '__workspace/test.ts';
    const getContent = () => 'const x = 1;';

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 10 })),
        },
      },
    };

    const result = await source(mockView, 5);
    expect(result).toBeNull();
  });

  // -------------------------------------------------------------------------
  // Language validation
  // -------------------------------------------------------------------------

  it('hover source returns null for non-hover languages (e.g., python)', async () => {
    const getFilePath = () => 'test.py';
    const getContent = () => 'print("hello")';

    mockResolveLanguageId.mockReturnValue({ languageId: 'python' });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 13 })),
        },
      },
    };

    const result = await source(mockView, 5);
    expect(result).toBeNull();
    expect(mockResolveLanguageId).toHaveBeenCalled();
  });

  it('hover source calls getSemanticHover for supported languages', async () => {
    const getFilePath = () => 'test.ts';
    const getContent = () => 'const x: number = 1;';

    mockResolveLanguageId.mockReturnValue({ languageId: 'typescript' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockResolvedValue({
        hover: { contents: 'number' },
      }),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 21 })),
        },
      },
    };

    await source(mockView, 5);

    const apiInstance = MockApiService.getInstance();
    expect(apiInstance.getSemanticHover).toHaveBeenCalledWith(
      'test.ts',
      'const x: number = 1;',
      'typescript',
      1, // line number
      6  // column (pos - line.from + 1, i.e., 5 - 0 + 1)
    );
  });

  // -------------------------------------------------------------------------
  // API result handling
  // -------------------------------------------------------------------------

  it('hover source returns null when API returns error', async () => {
    const getFilePath = () => 'test.ts';
    const getContent = () => 'const x = 1;';

    mockResolveLanguageId.mockReturnValue({ languageId: 'typescript' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockResolvedValue({
        error: 'some error',
      }),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 12 })),
        },
      },
    };

    const result = await source(mockView, 5);
    expect(result).toBeNull();
  });

  it('hover source returns null when API returns empty contents', async () => {
    const getFilePath = () => 'test.ts';
    const getContent = () => 'const x = 1;';

    mockResolveLanguageId.mockReturnValue({ languageId: 'typescript' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockResolvedValue({
        hover: { contents: '   ' },
      }),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 12 })),
        },
      },
    };

    const result = await source(mockView, 5);
    expect(result).toBeNull();
  });

  it('hover source returns null when API returns empty hover object', async () => {
    const getFilePath = () => 'test.ts';
    const getContent = () => 'const x = 1;';

    mockResolveLanguageId.mockReturnValue({ languageId: 'typescript' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockResolvedValue({}),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 12 })),
        },
      },
    };

    const result = await source(mockView, 5);
    expect(result).toBeNull();
  });

  // -------------------------------------------------------------------------
  // Tooltip result
  // -------------------------------------------------------------------------

  it('hover source returns tooltip result with correct pos', async () => {
    const getFilePath = () => 'test.ts';
    const getContent = () => 'const x = 1;';
    const pos = 5;

    mockResolveLanguageId.mockReturnValue({ languageId: 'typescript' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockResolvedValue({
        hover: { contents: 'number' },
      }),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 12 })),
        },
      },
    };

    const result = await source(mockView, pos);

    expect(result).not.toBeNull();
    expect(result.pos).toBe(pos);
    expect(result.create).toBeDefined();

    // Verify DOM creation
    const { dom } = result.create();
    expect(dom.className).toBe('cm-hover-tooltip');
    expect(dom.innerHTML).toBe('number');
  });

  // -------------------------------------------------------------------------
  // Error handling
  // -------------------------------------------------------------------------

  it('hover source catches errors and returns null', async () => {
    const getFilePath = () => 'test.ts';
    const getContent = () => 'const x = 1;';

    mockResolveLanguageId.mockReturnValue({ languageId: 'typescript' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockRejectedValue(new Error('network error')),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 12 })),
        },
      },
    };

    const result = await source(mockView, 5);

    expect(result).toBeNull();
    expect(mockDebugLog).toHaveBeenCalled();
  });

  // -------------------------------------------------------------------------
  // Position conversion
  // -------------------------------------------------------------------------

  it('correctly converts position to line:column for API', async () => {
    const getFilePath = () => 'test.ts';
    const getContent = () => 'line1\nline2';

    mockResolveLanguageId.mockReturnValue({ languageId: 'typescript' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockResolvedValue({
        hover: { contents: 'string' },
      }),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    // In "line1\nline2", position 7 is on line 2 (characters: l(6), i(7), n(8), e(9), 2(10))
    // lineAt(7) returns line 2 starting at pos 6, so column = 7 - 6 + 1 = 2
    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn((pos) => {
            // lineAt is called with the position being hovered
            if (pos === 7) return { number: 2, from: 6, to: 11 };
            return { number: 1, from: 0, to: 5 };
          }),
        },
      },
    };

    await source(mockView, 7);

    const apiInstance = MockApiService.getInstance();
    expect(apiInstance.getSemanticHover).toHaveBeenCalledWith(
      'test.ts',
      'line1\nline2',
      'typescript',
      2, // line number
      2  // column (7 - 6 + 1 = 2)
    );
  });

  // -------------------------------------------------------------------------
  // Supported languages
  // -------------------------------------------------------------------------

  it('works for javascript', async () => {
    const getFilePath = () => 'test.js';
    const getContent = () => 'const x = 1;';

    mockResolveLanguageId.mockReturnValue({ languageId: 'javascript' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockResolvedValue({
        hover: { contents: 'object' },
      }),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 12 })),
        },
      },
    };

    const result = await source(mockView, 5);
    expect(result).not.toBeNull();
  });

  it('works for javascript-jsx', async () => {
    const getFilePath = () => 'test.jsx';
    const getContent = () => '<div />';

    mockResolveLanguageId.mockReturnValue({ languageId: 'javascript-jsx' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockResolvedValue({
        hover: { contents: 'JSXElement' },
      }),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 6 })),
        },
      },
    };

    const result = await source(mockView, 3);
    expect(result).not.toBeNull();
  });

  it('works for go', async () => {
    const getFilePath = () => 'test.go';
    const getContent = () => 'package main';

    mockResolveLanguageId.mockReturnValue({ languageId: 'go' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockResolvedValue({
        hover: { contents: 'package main' },
      }),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 12 })),
        },
      },
    };

    const result = await source(mockView, 1);
    expect(result).not.toBeNull();
  });

  it('works for typescript-jsx', async () => {
    const getFilePath = () => 'test.tsx';
    const getContent = () => '<App />';

    mockResolveLanguageId.mockReturnValue({ languageId: 'typescript-jsx' });
    MockApiService.getInstance.mockReturnValue({
      getSemanticHover: jest.fn().mockResolvedValue({
        hover: { contents: 'React.FC' },
      }),
    });

    const extension = createHoverTooltipExtension(getFilePath, getContent);
    const source = mockHoverTooltip.mock.calls[0][0];

    const mockView = {
      state: {
        doc: {
          lineAt: jest.fn(() => ({ number: 1, from: 0, to: 6 })),
        },
      },
    };

    const result = await source(mockView, 1);
    expect(result).not.toBeNull();
  });
});