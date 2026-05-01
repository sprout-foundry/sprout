/**
 * errorLens.test.ts — Unit tests for the errorLens extension.
 *
 * Tests the exported pure helper functions:
 * - truncateMessage
 * - computeErrorLensDecorations
 *
 * CodeMirror modules are mocked since we test the pure computation logic
 * independently of the CodeMirror runtime.
 */

import { vi, describe, it, expect, beforeEach } from 'vitest';

// ── Mock CodeMirror modules ────────────────────────────────────────

vi.mock('./errorLens.css', () => ({}));

vi.mock('@codemirror/view', () => ({
  Decoration: {
    widget: vi.fn((opts) => ({
      _widget: opts.widget,
      range: vi.fn((from) => ({ from, to: from })),
    })),
    none: { type: 'none', iter: () => [][Symbol.iterator]() },
    set: vi.fn((ranges, sort) => ({ _ranges: ranges, _sort: sort })),
  },
  ViewPlugin: { fromClass: vi.fn(() => []) },
  EditorView: { baseTheme: vi.fn(() => []) },
  WidgetType: class {},
}));

vi.mock('@codemirror/state', () => ({
  Annotation: { define: vi.fn(() => ({})) },
}));

vi.mock('@codemirror/lint', () => ({
  forEachDiagnostic: vi.fn(),
  diagnosticCount: vi.fn(() => 0),
}));

vi.mock('../utils/log', () => ({
  debugLog: vi.fn(),
}));

// ── Module under test ──────────────────────────────────────────────
import { truncateMessage, computeErrorLensDecorations } from './errorLens';
import { forEachDiagnostic } from '@codemirror/lint';
import { Decoration as MockDecoration } from '@codemirror/view';

const mockForEachDiagnostic = vi.mocked(forEachDiagnostic);

// ── truncateMessage tests ─────────────────────────────────────────

describe('truncateMessage', () => {
  it('returns short messages unchanged', () => {
    expect(truncateMessage('Hello')).toBe('Hello');
  });

  it('returns messages at exactly MAX_MESSAGE_LENGTH unchanged', () => {
    const msg = 'a'.repeat(120);
    expect(truncateMessage(msg)).toBe(msg);
    expect(truncateMessage(msg).length).toBe(120);
  });

  it('truncates messages longer than MAX_MESSAGE_LENGTH and appends ellipsis', () => {
    const msg = 'a'.repeat(150);
    const result = truncateMessage(msg);
    expect(result.length).toBe(120);
    expect(result.endsWith('\u2026')).toBe(true);
    expect(result).toBe('a'.repeat(119) + '\u2026');
  });

  it('uses the provided maxLength when specified', () => {
    const msg = 'Hello, World!';
    expect(truncateMessage(msg, 5)).toBe('Hell\u2026');
  });

  it('handles empty string', () => {
    expect(truncateMessage('')).toBe('');
  });

  it('handles single character', () => {
    expect(truncateMessage('a')).toBe('a');
  });

  it('handles message just over the limit', () => {
    const msg = 'a'.repeat(121);
    const result = truncateMessage(msg);
    expect(result.length).toBe(120);
    expect(result.endsWith('\u2026')).toBe(true);
  });

  it('returns empty string when maxLength is 0', () => {
    expect(truncateMessage('Hello', 0)).toBe('');
  });

  it('returns empty string when maxLength is negative', () => {
    expect(truncateMessage('Hello', -1)).toBe('');
  });

  it('handles maxLength of 1', () => {
    expect(truncateMessage('ab', 1)).toBe('\u2026');
  });
});

// ── computeErrorLensDecorations tests ──────────────────────────────

/** Create a mock EditorView with 3-line document. */
function createMockView() {
  // Line 1: "function foo() {"  (0–15)
  // Line 2: "  return x;"       (16–26)
  // Line 3: "}"                  (27–27)
  const linePositions = [
    { start: 0, end: 15, number: 1 },
    { start: 16, end: 26, number: 2 },
    { start: 27, end: 27, number: 3 },
  ];

  return {
    viewport: { from: 0, to: 28 },
    state: {
      doc: {
        lineAt: vi.fn((pos) => {
          for (const lp of linePositions) {
            if (pos >= lp.start && pos <= lp.end) return { ...lp };
          }
          return { ...linePositions[linePositions.length - 1] };
        }),
        line: vi.fn((num) => {
          const idx = num - 1;
          if (idx < 0 || idx >= linePositions.length) return { from: 0, to: 0, number: 1 };
          return { ...linePositions[idx] };
        }),
        length: 28,
      },
    },
  };
}

describe('computeErrorLensDecorations', () => {
  beforeEach(() => {
    mockForEachDiagnostic.mockClear();
    MockDecoration.widget.mockClear();
    MockDecoration.set.mockClear();
  });

  it('returns Decoration.none when there are no diagnostics', () => {
    const view = createMockView();
    mockForEachDiagnostic.mockImplementation(() => {});

    const result = computeErrorLensDecorations(view);
    expect(result).toEqual({ type: 'none', iter: expect.any(Function) });
  });

  it('creates a widget for a single error diagnostic', () => {
    const view = createMockView();
    mockForEachDiagnostic.mockImplementation((_state, fn) => {
      fn({ from: 0, to: 10, severity: 'error', message: 'Unexpected token' });
    });

    computeErrorLensDecorations(view);
    expect(MockDecoration.set).toHaveBeenCalled();
  });

  it('groups multiple diagnostics on the same line', () => {
    const view = createMockView();
    mockForEachDiagnostic.mockImplementation((_state, fn) => {
      fn({ from: 0, to: 5, severity: 'error', message: 'Error 1' });
      fn({ from: 3, to: 8, severity: 'warning', message: 'Warning 1' });
    });

    computeErrorLensDecorations(view);

    const Decoration = MockDecoration;
    expect(Decoration.widget).toHaveBeenCalledTimes(1);
    const widgetOpts = Decoration.widget.mock.calls[0][0];
    expect(widgetOpts.widget.message).toContain('Error 1');
    expect(widgetOpts.widget.message).toContain('Warning 1');
    expect(widgetOpts.widget.message).toContain('\u2022');
  });

  it('uses the highest severity when multiple diagnostics share a line', () => {
    const view = createMockView();
    mockForEachDiagnostic.mockImplementation((_state, fn) => {
      fn({ from: 0, to: 5, severity: 'warning', message: 'Warn' });
      fn({ from: 3, to: 8, severity: 'error', message: 'Err' });
    });

    computeErrorLensDecorations(view);

    const widgetOpts = MockDecoration.widget.mock.calls[0][0];
    expect(widgetOpts.widget.severity).toBe('error');
  });

  it('preserves error severity over warning, info, and hint', () => {
    const view = createMockView();
    mockForEachDiagnostic.mockImplementation((_state, fn) => {
      fn({ from: 0, to: 2, severity: 'hint', message: 'H' });
      fn({ from: 2, to: 4, severity: 'info', message: 'I' });
      fn({ from: 4, to: 6, severity: 'warning', message: 'W' });
      fn({ from: 6, to: 8, severity: 'error', message: 'E' });
    });

    computeErrorLensDecorations(view);

    const widgetOpts = MockDecoration.widget.mock.calls[0][0];
    expect(widgetOpts.widget.severity).toBe('error');
  });

  it('creates separate widgets for diagnostics on different lines', () => {
    const view = createMockView();
    mockForEachDiagnostic.mockImplementation((_state, fn) => {
      fn({ from: 0, to: 5, severity: 'error', message: 'Line 1 error' });
      fn({ from: 27, to: 27, severity: 'warning', message: 'Line 3 warning' });
    });

    computeErrorLensDecorations(view);

    expect(MockDecoration.widget).toHaveBeenCalledTimes(2);
  });

  it('skips diagnostics outside the viewport', () => {
    const view = {
      viewport: { from: 16, to: 26 },
      state: {
        doc: {
          lineAt: vi.fn((pos) => {
            if (pos <= 15) return { from: 0, to: 15, number: 1 };
            if (pos <= 26) return { from: 16, to: 26, number: 2 };
            return { from: 27, to: 27, number: 3 };
          }),
          line: vi.fn((num) => {
            if (num === 1) return { from: 0, to: 15, number: 1 };
            if (num === 2) return { from: 16, to: 26, number: 2 };
            return { from: 27, to: 27, number: 3 };
          }),
          length: 28,
        },
      },
    };

    mockForEachDiagnostic.mockImplementation((_state, fn) => {
      fn({ from: 0, to: 5, severity: 'error', message: 'Outside viewport' });
      fn({ from: 18, to: 22, severity: 'warning', message: 'Inside viewport' });
    });

    computeErrorLensDecorations(view);

    const Decoration = MockDecoration;
    expect(Decoration.widget).toHaveBeenCalledTimes(1);
    expect(Decoration.widget.mock.calls[0][0].widget.message).toBe('Inside viewport');
  });

  it('truncates long diagnostic messages', () => {
    const longMessage = 'A'.repeat(200);
    const view = createMockView();
    mockForEachDiagnostic.mockImplementation((_state, fn) => {
      fn({ from: 0, to: 5, severity: 'error', message: longMessage });
    });

    computeErrorLensDecorations(view);

    const widgetOpts = MockDecoration.widget.mock.calls[0][0];
    expect(widgetOpts.widget.message.length).toBe(120);
    expect(widgetOpts.widget.message.endsWith('\u2026')).toBe(true);
  });

  it('truncates the combined message when multiple diagnostics produce a very long string', () => {
    const longMessages = Array.from({ length: 10 }, (_, i) => `Error ${i}: ${'X'.repeat(50)}`);
    const view = createMockView();
    mockForEachDiagnostic.mockImplementation((_state, fn) => {
      for (const msg of longMessages) {
        fn({ from: 0, to: 5, severity: 'error', message: msg });
      }
    });

    computeErrorLensDecorations(view);

    const widgetOpts = MockDecoration.widget.mock.calls[0][0];
    // Combined message should be truncated to MAX_COMBINED_LENGTH (200)
    expect(widgetOpts.widget.message.length).toBe(200);
    expect(widgetOpts.widget.message.endsWith('\u2026')).toBe(true);
  });

  it('handles all four severity levels', () => {
    const severities = ['error', 'warning', 'info', 'hint'];
    for (const sev of severities) {
      vi.clearAllMocks();
      const view = createMockView();
      mockForEachDiagnostic.mockImplementation((_state, fn) => {
        fn({ from: 0, to: 3, severity: sev, message: `${sev} msg` });
      });

      computeErrorLensDecorations(view);

      const widgetOpts = MockDecoration.widget.mock.calls[0][0];
      expect(widgetOpts.widget.severity).toBe(sev);
    }
  });

  it('maps unknown severity to "info"', () => {
    const view = createMockView();
    mockForEachDiagnostic.mockImplementation((_state, fn) => {
      fn({ from: 0, to: 3, severity: 'unknown', message: 'Unknown severity' });
    });

    computeErrorLensDecorations(view);

    const widgetOpts = MockDecoration.widget.mock.calls[0][0];
    expect(widgetOpts.widget.severity).toBe('info');
  });
});
