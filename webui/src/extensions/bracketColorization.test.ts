/**
 * bracketColorization.test.ts — Unit tests for the bracketColorization extension.
 *
 * Since CodeMirror 6 modules use ESM and cannot load in Jest 27.x,
 * the three CM imports are mocked.  We test the exported
 * `computeBracketDecorations` helper directly — it's a pure function
 * with no CM dependencies.
 */

// ── Module under test (Jest hoists mocks above imports) ─────────────
import { computeBracketDecorations, MAX_DEPTH } from './bracketColorization';

// ── Mock CodeMirror modules (ESM internals break Jest 27) ───────────

jest.mock('@codemirror/view', () => ({
  Decoration: { mark: jest.fn(() => ({ range: jest.fn() })), set: jest.fn(), none: [] },
  ViewPlugin: { fromClass: jest.fn() },
  EditorView: { baseTheme: jest.fn(() => []) },
}));
jest.mock('@codemirror/state', () => ({}));

// ── computeBracketDecorations tests ─────────────────────────────────

describe('computeBracketDecorations', () => {
  // -------------------------------------------------------------------------
  // Empty / trivial inputs
  // -------------------------------------------------------------------------

  it('returns no decorations for an empty string', () => {
    expect(computeBracketDecorations('')).toEqual([]);
  });

  it('returns no decorations for text with no brackets', () => {
    expect(computeBracketDecorations('no brackets here')).toEqual([]);
  });

  it('returns no decorations for plain text with spaces', () => {
    expect(computeBracketDecorations('hello world 123')).toEqual([]);
  });

  // -------------------------------------------------------------------------
  // Simple matched pairs
  // -------------------------------------------------------------------------

  it('colors a single pair of parentheses at depth 0', () => {
    const result = computeBracketDecorations('(hello)');
    expect(result).toHaveLength(2);
    // Opening ( at position 0
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 });
    // Closing ) at position 6
    expect(result[1]).toEqual({ from: 6, to: 7, depth: 0 });
  });

  it('colors a single pair of square brackets at depth 0', () => {
    const result = computeBracketDecorations('[array]');
    expect(result).toHaveLength(2);
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 });
    expect(result[1]).toEqual({ from: 6, to: 7, depth: 0 });
  });

  it('colors a single pair of curly braces at depth 0', () => {
    const result = computeBracketDecorations('{obj}');
    expect(result).toHaveLength(2);
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 });
    expect(result[1]).toEqual({ from: 4, to: 5, depth: 0 });
  });

  // -------------------------------------------------------------------------
  // Nested brackets — increasing depth
  // -------------------------------------------------------------------------

  it('assigns increasing depth for nested brackets: { ( [ ] ) }', () => {
    const text = '{ ( [ ] ) }';
    const result = computeBracketDecorations(text);

    // Strip spaces to get the 6 bracket positions, or just check the
    // result entries directly.
    // text = '{ ( [ ] ) }'
    // chars: 0='{', 2='(', 4='[', 6=']', 8=')', 10='}'
    expect(result).toHaveLength(6);

    // { at depth 0
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 });
    // ( at depth 1
    expect(result[1]).toEqual({ from: 2, to: 3, depth: 1 });
    // [ at depth 2
    expect(result[2]).toEqual({ from: 4, to: 5, depth: 2 });
    // ] at depth 2 (matches [)
    expect(result[3]).toEqual({ from: 6, to: 7, depth: 2 });
    // ) at depth 1 (matches ()
    expect(result[4]).toEqual({ from: 8, to: 9, depth: 1 });
    // } at depth 0 (matches {)
    expect(result[5]).toEqual({ from: 10, to: 11, depth: 0 });
  });

  it('handles deeply nested: (((x)))', () => {
    const result = computeBracketDecorations('(((x)))');
    expect(result).toHaveLength(6);
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 });
    expect(result[1]).toEqual({ from: 1, to: 2, depth: 1 });
    expect(result[2]).toEqual({ from: 2, to: 3, depth: 2 });
    expect(result[3]).toEqual({ from: 4, to: 5, depth: 2 });
    expect(result[4]).toEqual({ from: 5, to: 6, depth: 1 });
    expect(result[5]).toEqual({ from: 6, to: 7, depth: 0 });
  });

  // -------------------------------------------------------------------------
  // Depth wraps at 6 (modulo)
  // -------------------------------------------------------------------------

  it('wraps depth at 6 levels deep', () => {
    // 7 opening parens → depths 0,1,2,3,4,5,0
    const text = '((((((()))))))';
    const result = computeBracketDecorations(text);

    // 14 brackets total
    expect(result).toHaveLength(14);

    // First 7 openings: depths 0-5, then 0
    expect(result[0].depth).toBe(0);
    expect(result[1].depth).toBe(1);
    expect(result[2].depth).toBe(2);
    expect(result[3].depth).toBe(3);
    expect(result[4].depth).toBe(4);
    expect(result[5].depth).toBe(5);
    expect(result[6].depth).toBe(0); // wrapped

    // Closers mirror the openers (last 7)
    expect(result[7].depth).toBe(0); // matches 7th opener
    expect(result[8].depth).toBe(5);
    expect(result[9].depth).toBe(4);
    expect(result[10].depth).toBe(3);
    expect(result[11].depth).toBe(2);
    expect(result[12].depth).toBe(1);
    expect(result[13].depth).toBe(0);
  });

  // -------------------------------------------------------------------------
  // Unmatched opening brackets
  // -------------------------------------------------------------------------

  it('colors unmatched opening brackets', () => {
    // (() — inner () matched, outer ( unmatched
    const result = computeBracketDecorations('(()');
    expect(result).toHaveLength(3);
    // Outer ( at depth 0
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 });
    // Inner ( at depth 1
    expect(result[1]).toEqual({ from: 1, to: 2, depth: 1 });
    // ) matches inner ( at depth 1
    expect(result[2]).toEqual({ from: 2, to: 3, depth: 1 });
  });

  it('colors a single unmatched opening bracket', () => {
    const result = computeBracketDecorations('x(y');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ from: 1, to: 2, depth: 0 });
  });

  it('colors multiple unmatched opening brackets', () => {
    // ]([  — ] is stray closer (ignored), ( and [ are push onto stack
    // Actually ] is unmatched closer, so it's ignored. ( and [ are openers.
    const result = computeBracketDecorations(']([');
    // ( at pos 1, depth 0; [ at pos 2, depth 1
    expect(result).toHaveLength(2);
    expect(result[0]).toEqual({ from: 1, to: 2, depth: 0 });
    expect(result[1]).toEqual({ from: 2, to: 3, depth: 1 });
  });

  // -------------------------------------------------------------------------
  // Unmatched / mismatched closing brackets
  // -------------------------------------------------------------------------

  it('ignores stray closing brackets when stack is empty', () => {
    const result = computeBracketDecorations('])}');
    // Empty string of text with only closers, stack always empty
    expect(result).toHaveLength(0);
  });

  it('ignores mismatched closing bracket types', () => {
    // ([)] — ] does not match ( (top of stack), so ] is ignored.
    // ) also fails because top of stack is [ not (.  Stack ends with [(].
    const result = computeBracketDecorations('([)]');
    // ( at 0 depth 0, [ at 1 depth 1 — both are openers.
    // ] at 2: top is [, but char is ]. DOES match! Wait...
    // Actually text is '([)]' — chars: (, [, ), ]
    // ( push depth 0
    // [ push depth 1
    // ) — top is [, not (, so IGNORED
    // ] — top is [, match! Pop. depth 1
    expect(result).toHaveLength(3);
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 }); // (
    expect(result[1]).toEqual({ from: 1, to: 2, depth: 1 }); // [
    // result[2] is ] which matches [ at depth 1
    expect(result[2]).toEqual({ from: 3, to: 4, depth: 1 });
  });

  it('handles multiple stray closers in sequence', () => {
    const result = computeBracketDecorations(')))');
    expect(result).toHaveLength(0);
  });

  // -------------------------------------------------------------------------
  // Angle brackets — should NOT be colored
  // -------------------------------------------------------------------------

  it('does not color angle brackets', () => {
    const result = computeBracketDecorations('<angle>');
    expect(result).toHaveLength(0);
  });

  it('does not color angle brackets mixed with real brackets', () => {
    // <(text)> — only () should be colored, not <>
    const result = computeBracketDecorations('<(text)>');
    expect(result).toHaveLength(2);
    expect(result[0]).toEqual({ from: 1, to: 2, depth: 0 });
    expect(result[1]).toEqual({ from: 6, to: 7, depth: 0 });
  });

  it('does not color comparison operators', () => {
    const result = computeBracketDecorations('x < y && z > w');
    expect(result).toHaveLength(0);
  });

  // -------------------------------------------------------------------------
  // Mixed bracket types
  // -------------------------------------------------------------------------

  it('correctly matches different bracket types in nesting', () => {
    // {[]} — nested square inside curly
    const result = computeBracketDecorations('{}[]');
    expect(result).toHaveLength(4);
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 }); // {
    expect(result[1]).toEqual({ from: 1, to: 2, depth: 0 }); // }
    expect(result[2]).toEqual({ from: 2, to: 3, depth: 0 }); // [
    expect(result[3]).toEqual({ from: 3, to: 4, depth: 0 }); // ]
  });

  it('handles interleaved types: ([]){}', () => {
    // ([])  — ( at 0 depth 0, [ at 1 depth 1, ] at 2 depth 1, ) at 3 depth 0
    const result = computeBracketDecorations('([]){}');
    expect(result).toHaveLength(6);
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 }); // (
    expect(result[1]).toEqual({ from: 1, to: 2, depth: 1 }); // [
    expect(result[2]).toEqual({ from: 2, to: 3, depth: 1 }); // ]
    expect(result[3]).toEqual({ from: 3, to: 4, depth: 0 }); // )
    expect(result[4]).toEqual({ from: 4, to: 5, depth: 0 }); // {
    expect(result[5]).toEqual({ from: 5, to: 6, depth: 0 }); // }
  });

  // -------------------------------------------------------------------------
  // Practical code examples
  // -------------------------------------------------------------------------

  it('handles a function call with arguments', () => {
    const text = 'foo("bar", [1, 2])';
    const result = computeBracketDecorations(text);
    // f(0)o(1)o(2)((3)"(4)…(8),(9) (10)[(11)1(12),(13) (14)2(15)](16))(17)
    // ( at pos 3 depth 0, [ at pos 11 depth 1, ] at pos 16 depth 1, ) at pos 17 depth 0
    expect(result).toHaveLength(4);
    expect(result[0]).toEqual({ from: 3, to: 4, depth: 0 }); // (
    expect(result[1]).toEqual({ from: 11, to: 12, depth: 1 }); // [
    expect(result[2]).toEqual({ from: 16, to: 17, depth: 1 }); // ]
    expect(result[3]).toEqual({ from: 17, to: 18, depth: 0 }); // )
  });

  it('handles TypeScript generics with real brackets', () => {
    const text = 'fn<string[]>(value)';
    // The [] inside <string[]> are real brackets our scanner picks up.
    // The algorithm is character-based, not syntax-aware.
    const result = computeBracketDecorations(text);
    // [ at 9 depth 0, ] at 10 depth 0, ( at 12 depth 0, ) at 18 depth 0
    expect(result).toHaveLength(4);
    expect(result[0]).toEqual({ from: 9, to: 10, depth: 0 }); // [
    expect(result[1]).toEqual({ from: 10, to: 11, depth: 0 }); // ]
    expect(result[2]).toEqual({ from: 12, to: 13, depth: 0 }); // (
    expect(result[3]).toEqual({ from: 18, to: 19, depth: 0 }); // )
  });

  // -------------------------------------------------------------------------
  // String-like content (brackets inside strings are still colored)
  // -------------------------------------------------------------------------

  it('colors brackets inside string-like content (naive, no string awareness)', () => {
    // The algorithm is purely character-based; it doesn't understand
    // string delimiters.  This is by design — the syntax highlighter
    // handles meaning; we only handle nesting depth.
    const result = computeBracketDecorations('"unmatched("');
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ from: 10, to: 11, depth: 0 });
  });

  // -------------------------------------------------------------------------
  // Additional edge cases from code review
  // -------------------------------------------------------------------------

  it('recovers from mismatch then match: ([)])', () => {
    // ( push 0, [ push 1, ) mismatch (top=[) ignore, ] matches [ pop 1, ) matches ( pop 0
    const result = computeBracketDecorations('([)])');
    expect(result).toHaveLength(4);
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 }); // (
    expect(result[1]).toEqual({ from: 1, to: 2, depth: 1 }); // [
    expect(result[2]).toEqual({ from: 3, to: 4, depth: 1 }); // ] matches [
    expect(result[3]).toEqual({ from: 4, to: 5, depth: 0 }); // ) matches (
  });

  it('mismatched closer does not corrupt subsequent matches: (])', () => {
    // ( push 0, ] mismatch ignore, ) matches ( pop 0
    const result = computeBracketDecorations('(])');
    expect(result).toHaveLength(2);
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 }); // (
    expect(result[1]).toEqual({ from: 2, to: 3, depth: 0 }); // ) matches (
  });

  it('handles empty brackets with zero-length content', () => {
    const result = computeBracketDecorations('()');
    expect(result).toHaveLength(2);
    expect(result[0]).toEqual({ from: 0, to: 1, depth: 0 });
    expect(result[1]).toEqual({ from: 1, to: 2, depth: 0 });
  });
});

// ── Constants ──────────────────────────────────────────────────────────

describe('exported constants', () => {
  it('exports MAX_DEPTH as 6', () => {
    expect(MAX_DEPTH).toBe(6);
  });
});
