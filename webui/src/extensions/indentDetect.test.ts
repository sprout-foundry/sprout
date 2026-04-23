/**
 * indentDetect.test.ts — Unit tests for the indentDetect extension.
 */

import { detectIndentation, DEFAULT_INDENT_WIDTH } from './indentDetect';

describe('detectIndentation', () => {
  // -------------------------------------------------------------------------
  // Empty / trivial inputs
  // -------------------------------------------------------------------------

  it('returns default for empty string', () => {
    const result = detectIndentation('');
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(DEFAULT_INDENT_WIDTH);
    expect(result.linesAnalyzed).toBe(0);
  });

  it('returns default for whitespace-only string', () => {
    // "   \n   \t  " splits to 2 lines: "   " and "   \t  "
    const result = detectIndentation('   \n   \t  ');
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(DEFAULT_INDENT_WIDTH);
    expect(result.linesAnalyzed).toBe(2);
  });

  it('returns default for no indentation (no leading whitespace)', () => {
    const result = detectIndentation('hello\nworld\nfoo bar');
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(DEFAULT_INDENT_WIDTH);
    expect(result.linesAnalyzed).toBe(3);
    expect(result.indentedLineCount).toBe(0);
  });

  // -------------------------------------------------------------------------
  // Pure tabs
  // -------------------------------------------------------------------------

  it('detects tab-indented file (pure tabs)', () => {
    const content = '\tfunction foo() {\n\t\tconst x = 1;\n\t\treturn x;\n\t}';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(true);
    expect(result.indentWidth).toBe(DEFAULT_INDENT_WIDTH);
    expect(result.linesAnalyzed).toBe(4);
    expect(result.indentedLineCount).toBe(4);
  });

  it('detects tab-indented file with many lines', () => {
    const lines = [
      '\tconst a = 1;',
      '\tconst b = 2;',
      '\tfunction test() {',
      '\t\treturn a + b;',
      '\t}',
    ];
    const result = detectIndentation(lines.join('\n'));
    expect(result.useTabs).toBe(true);
    expect(result.linesAnalyzed).toBe(5);
  });

  // -------------------------------------------------------------------------
  // Pure spaces - 2
  // -------------------------------------------------------------------------

  it('detects 2-space indentation', () => {
    const content = 'function foo() {\n  const x = 1;\n  return x;\n}';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(2);
    expect(result.linesAnalyzed).toBe(4);
  });

  it('detects 2-space indentation with many lines', () => {
    const lines = [
      'function test() {',
      '  const a = 1;',
      '  const b = 2;',
      '  return a + b;',
      '}',
    ];
    const result = detectIndentation(lines.join('\n'));
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(2);
    expect(result.linesAnalyzed).toBe(5);
  });

  // -------------------------------------------------------------------------
  // Pure spaces - 4 (default)
  // -------------------------------------------------------------------------

  it('detects 4-space indentation', () => {
    const content = 'function foo() {\n    const x = 1;\n    return x;\n}';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(4);
    expect(result.linesAnalyzed).toBe(4);
    expect(result.indentedLineCount).toBe(2);
  });

  it('detects 4-space indentation with many lines', () => {
    const lines = [
        'class Foo {',
        '    constructor() {',
        '        this.x = 1;',
        '    }',
        '}',
    ];
    const result = detectIndentation(lines.join('\n'));
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(4);
  });

  // -------------------------------------------------------------------------
  // Pure spaces - 8
  // -------------------------------------------------------------------------

  it('detects 8-space indentation', () => {
    const content = 'function foo() {\n        const x = 1;\n        return x;\n}';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(8);
    expect(result.linesAnalyzed).toBe(4);
  });

  // -------------------------------------------------------------------------
  // Mixed indentation - tabs win
  // -------------------------------------------------------------------------

  it('detects mixed indentation where tabs win (more tab lines)', () => {
    // 3 tab-indented lines, 2 space-indented lines
    const content = '\tconst a = 1;\n\tconst b = 2;\n  const c = 3;\n  const d = 4;\n\tconst e = 5;';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(true);
    expect(result.linesAnalyzed).toBe(5);
  });

  // -------------------------------------------------------------------------
  // Mixed indentation - spaces win (ties go to spaces)
  // -------------------------------------------------------------------------

  it('detects mixed indentation where spaces win', () => {
    // 2 tab-indented, 3 space-indented → spaces win (majority)
    const content = '\tconst a = 1;\n  const b = 2;\n  const c = 3;\n  const d = 4;\n\tconst e = 5;';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(2);
    expect(result.linesAnalyzed).toBe(5);
  });

  it('handles tie (tabs vs spaces) - spaces win', () => {
    // 2 tabs, 2 spaces → tie goes to spaces
    const content = '\tconst a = 1;\n\tconst b = 2;\n  const c = 3;\n  const d = 4;';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(2);
    expect(result.linesAnalyzed).toBe(4);
  });

  // -------------------------------------------------------------------------
  // No indentation / empty lines
  // -------------------------------------------------------------------------

  it('returns default for file with no indentation (all blank lines)', () => {
    // "\n\n\n\n" splits into 5 empty lines
    const content = '\n\n\n\n';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(DEFAULT_INDENT_WIDTH);
    expect(result.linesAnalyzed).toBe(5);
  });

  it('returns default when only 1 line has indentation', () => {
    const content = 'function foo() {\n    return 1;\n}';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(DEFAULT_INDENT_WIDTH);
    expect(result.linesAnalyzed).toBe(3);
  });

  // -------------------------------------------------------------------------
  // Very short files
  // -------------------------------------------------------------------------

  it('handles single line with no indentation', () => {
    const result = detectIndentation('x');
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(DEFAULT_INDENT_WIDTH);
    expect(result.linesAnalyzed).toBe(1);
  });

  it('handles single line with indentation', () => {
    const result = detectIndentation('  x');
    // Only 1 indented line → returns default
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(DEFAULT_INDENT_WIDTH);
    expect(result.linesAnalyzed).toBe(1);
  });

  it('handles two lines - one indented, one not', () => {
    const content = '  x\ny';
    const result = detectIndentation(content);
    // Only 1 indented line → returns default
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(DEFAULT_INDENT_WIDTH);
    expect(result.linesAnalyzed).toBe(2);
  });

  // -------------------------------------------------------------------------
  // Mixed tab+space on same line (tab wins for that line)
  // -------------------------------------------------------------------------

  it('counts lines starting with tab as tab even if followed by spaces', () => {
    // Line starts with tab → counts as tab
    // This has 2 tabs (\t and \t\t) and 2 spaces (2 each) → tie → spaces win per spec
    const content = '\t   const a = 1;\n  const b = 2;\n\t\tconst c = 3;\n  const d = 4;';
    const result = detectIndentation(content);
    // Tie: 2 tabs, 2 spaces → spaces win (default tie-break rule)
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(2);
  });

  // -------------------------------------------------------------------------
  // Non-standard space widths (should snap)
  // -------------------------------------------------------------------------

  it('snaps 3 spaces to 2 (closest)', () => {
    const content = '   x\n   y';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    // 3 → closest is 2 or 4, should be 2
    expect(result.indentWidth).toBe(2);
  });

  it('snaps 6 spaces to 2 (largest divisor)', () => {
    // GCD = 6, which is evenly divisible by 2 but not 4 or 8
    // So it returns the largest standard width that divides evenly → 2
    const content = '      x\n      y';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(2);
  });

  it('snaps 5 spaces to 4 (closest)', () => {
    const content = '     x\n     y';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    // 5 → closest is 4
    expect(result.indentWidth).toBe(4);
  });

  // -------------------------------------------------------------------------
  // GCD computation
  // -------------------------------------------------------------------------

  it('computes GCD from lines with 4, 8, 12 spaces → width 4', () => {
    const content = '    x\n        y\n            z';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    // GCD(4, 8, 12) = 4
    expect(result.indentWidth).toBe(4);
  });

  it('computes GCD of 6 and 8 → width 2', () => {
    const content = '      x\n        y';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    // GCD(6, 8) = 2
    expect(result.indentWidth).toBe(2);
  });

  it('computes GCD with varying space counts that share common divisor', () => {
    const content = '  x\n    y\n      z\n        w';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    // GCD(2, 4, 6, 8) = 2
    expect(result.indentWidth).toBe(2);
  });

  it('computes GCD of 4 and 6 → width 2', () => {
    const content = '    x\n      y';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(2);
  });

  // -------------------------------------------------------------------------
  // Custom maxLines parameter
  // -------------------------------------------------------------------------

  it('respects custom maxLines parameter', () => {
    const content = '    x\n    y\n    z\n    w\n    v';
    const result = detectIndentation(content, 3);
    expect(result.linesAnalyzed).toBe(3);
    expect(result.indentWidth).toBe(4);
  });

  it('handles maxLines smaller than content', () => {
    const lines = [
      '    line1',
      '    line2',
      '    line3',
      '    line4',
      '    line5',
    ];
    const content = lines.join('\n');
    const result = detectIndentation(content, 2);
    expect(result.linesAnalyzed).toBe(2);
    expect(result.indentWidth).toBe(4);
  });

  // -------------------------------------------------------------------------
  // linesAnalyzed reflects actual lines scanned
  // -------------------------------------------------------------------------

  it('sets linesAnalyzed correctly when content has fewer lines than maxLines', () => {
    const content = '  x\n  y';
    const result = detectIndentation(content, 100);
    expect(result.linesAnalyzed).toBe(2);
  });

  it('sets linesAnalyzed correctly when content has more lines than maxLines', () => {
    const lines = Array(150).fill('    x');
    const content = lines.join('\n');
    const result = detectIndentation(content, 100);
    expect(result.linesAnalyzed).toBe(100);
  });

  // -------------------------------------------------------------------------
  // Edge cases
  // -------------------------------------------------------------------------

  it('handles file with trailing newline', () => {
    const content = '    x\n    y\n';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(4);
    expect(result.linesAnalyzed).toBe(3);
  });

  it('handles file with only whitespace on some lines', () => {
    const content = '    x\n   \n    y';
    const result = detectIndentation(content);
    expect(result.useTabs).toBe(false);
    expect(result.indentWidth).toBe(4);
    expect(result.linesAnalyzed).toBe(3);
  });

  it('handles mixed tabs and spaces but only 1 space-indented line', () => {
    const content = '\tconst a = 1;\n  const b = 2;\n\tconst c = 3;';
    const result = detectIndentation(content);
    // 2 tabs, 1 space → tabs win
    expect(result.useTabs).toBe(true);
    expect(result.linesAnalyzed).toBe(3);
  });

  // -------------------------------------------------------------------------
  // indentedLineCount
  // -------------------------------------------------------------------------

  it('counts indentedLineCount correctly for mixed indentation', () => {
    // 3 tab-indented lines, 2 space-indented lines, 1 plain line = 5 indented
    const content = 'header\n\tindented1\n  indented2\n\tindented3\n  indented4\n\tindented5';
    const result = detectIndentation(content);
    expect(result.indentedLineCount).toBe(5);
    expect(result.linesAnalyzed).toBe(6);
  });

  it('returns indentedLineCount of 0 for no indentation', () => {
    const result = detectIndentation('foo\nbar\nbaz');
    expect(result.indentedLineCount).toBe(0);
  });

  it('returns indentedLineCount of 1 for single indented line', () => {
    const result = detectIndentation('  x\ny');
    expect(result.indentedLineCount).toBe(1);
  });
});