// @ts-nocheck

import { extractResultSummary, getSubagentResultPreview, formatToolDetail, truncateText } from './resultSummary';

describe('resultSummary', () => {
  describe('truncateText', () => {
    it('returns short text unchanged', () => {
      expect(truncateText('short', 100)).toBe('short');
    });

    it('returns text unchanged when exactly at maxLength', () => {
      expect(truncateText('abc', 3)).toBe('abc');
    });

    it('truncates long text with "..."', () => {
      // Truncates to maxLength-1 chars + "..." = maxLength+1 total
      expect(truncateText('hello world this is a very long text', 10)).toBe('hello wor...');
    });

    it('handles exact maxLength boundary', () => {
      expect(truncateText('hello world', 11)).toBe('hello world');
      expect(truncateText('hello world', 10)).toBe('hello wor...');
    });

    it('handles empty string', () => {
      expect(truncateText('', 10)).toBe('');
    });

    it('handles whitespace-only string', () => {
      expect(truncateText('   ', 10)).toBe('   ');
    });

    it('handles maxLength of 0', () => {
      // When maxLength is 0, we get "" (slice 0,0) + "..." = "..."
      expect(truncateText('hello', 0)).toBe('...');
    });

    it('handles maxLength of 1', () => {
      // When maxLength is 1, we get "" (slice 0,0) + "..." = "..."
      expect(truncateText('hello', 1)).toBe('...');
    });
  });

  describe('formatToolDetail', () => {
    it('pretty-prints valid JSON', () => {
      const input = JSON.stringify({ key: 'value', num: 42 }, null, 2);
      const result = formatToolDetail(input);
      expect(result).toContain('  "key": "value"');
      expect(result).toContain('  "num": 42');
      expect(result).toContain('\n');
    });

    it('returns string as-is for invalid JSON', () => {
      expect(formatToolDetail('not valid json')).toBe('not valid json');
    });

    it('handles JSON with nested objects', () => {
      const input = JSON.stringify({ a: { b: { c: 1 } } }, null, 2);
      const result = formatToolDetail(input);
      expect(result).toContain('"a"');
      expect(result).toContain('"b"');
      expect(result).toContain('1');
    });

    it('handles JSON arrays', () => {
      const input = JSON.stringify([1, 2, 3], null, 2);
      const result = formatToolDetail(input);
      expect(result).toContain('1');
      expect(result).toContain('2');
      expect(result).toContain('3');
    });

    it('handles JSON with special characters', () => {
      const input = JSON.stringify({ msg: 'hello <world> & test' }, null, 2);
      const result = formatToolDetail(input);
      // JSON.stringify doesn't escape HTML entities, so we check for the raw characters
      expect(result).toContain('hello');
      expect(result).toContain('<world>');
      expect(result).toContain('& test');
    });
  });

  describe('extractResultSummary', () => {
    // String tests
    it('returns trimmed string for simple string input', () => {
      expect(extractResultSummary('hello world')).toBe('hello world');
    });

    it('strips ANSI codes from string input', () => {
      // Using a simple ANSI color code
      expect(extractResultSummary('\x1B[31mred\x1B[0m')).toBe('red');
    });

    it('returns null for empty strings', () => {
      expect(extractResultSummary('')).toBe(null);
    });

    it('returns null for whitespace-only strings', () => {
      expect(extractResultSummary('   \n\t  ')).toBe(null);
    });

    it('returns trimmed string after stripping ANSI and normalizing whitespace', () => {
      expect(extractResultSummary('\x1B[31m  hello   world  \x1B[0m')).toBe('hello world');
    });

    // Number tests
    it('converts number to string', () => {
      expect(extractResultSummary(42)).toBe('42');
    });

    it('converts negative number to string', () => {
      expect(extractResultSummary(-123)).toBe('-123');
    });

    it('converts float to string', () => {
      expect(extractResultSummary(3.14159)).toBe('3.14159');
    });

    // Boolean tests
    it('converts boolean true to string', () => {
      expect(extractResultSummary(true)).toBe('true');
    });

    it('converts boolean false to string', () => {
      expect(extractResultSummary(false)).toBe('false');
    });

    // Array tests
    it('returns first non-null summary for arrays', () => {
      expect(extractResultSummary(['first', 'second', 'third'])).toBe('first');
    });

    it('returns null for empty arrays', () => {
      expect(extractResultSummary([])).toBe(null);
    });

    it('returns nested value for arrays of arrays', () => {
      expect(extractResultSummary([[1, 2, 3]])).toBe('1');
    });

    it('returns summary from array containing mixed types', () => {
      expect(extractResultSummary([null, undefined, 'found', 42])).toBe('found');
    });

    // Object tests - priority keys
    it('checks priority keys first for objects', () => {
      expect(extractResultSummary({ summary: 'summary value', other: 'other' })).toBe('summary value');
    });

    it('returns value from "result" key', () => {
      expect(extractResultSummary({ result: 'result value' })).toBe('result value');
    });

    it('returns value from "response" key', () => {
      expect(extractResultSummary({ response: 'response value' })).toBe('response value');
    });

    it('returns value from "output" key', () => {
      expect(extractResultSummary({ output: 'output value' })).toBe('output value');
    });

    it('returns value from "final_answer" key', () => {
      expect(extractResultSummary({ final_answer: 'final_answer value' })).toBe('final_answer value');
    });

    it('returns value from "message" key', () => {
      expect(extractResultSummary({ message: 'message value' })).toBe('message value');
    });

    it('returns value from "content" key', () => {
      expect(extractResultSummary({ content: 'content value' })).toBe('content value');
    });

    it('returns value from non-priority keys in flat objects', () => {
      // Falls back to checking all values in object
      expect(extractResultSummary({ other: 'value' })).toBe('value');
    });

    // Object tests - fallback to non-priority keys
    it('falls back to non-priority keys in nested objects', () => {
      expect(extractResultSummary({ outer: { result: 'nested value' } })).toBe('nested value');
    });

    it('returns value from nested object with priority key', () => {
      expect(extractResultSummary({ outer: { summary: 'nested summary' } })).toBe('nested summary');
    });

    it('returns null for empty objects', () => {
      expect(extractResultSummary({})).toBe(null);
    });

    it('handles deeply nested objects', () => {
      expect(extractResultSummary({ a: { b: { c: { summary: 'deep' } } } })).toBe('deep');
    });

    it('returns null for null value', () => {
      expect(extractResultSummary(null)).toBe(null);
    });

    it('returns null for undefined value', () => {
      expect(extractResultSummary(undefined)).toBe(null);
    });

    it('handles ANSI codes inside nested values', () => {
      expect(extractResultSummary({ summary: '\x1B[31mcolored\x1B[0m' })).toBe('colored');
    });

    it('returns string from non-priority fields with ANSI codes', () => {
      expect(extractResultSummary({ other: '\x1B[31mcolored' })).toBe('colored');
    });

    it('handles arrays with mixed nested structures', () => {
      expect(extractResultSummary([null, { summary: 'found' }, []])).toBe('found');
    });

    it('returns null for array with only null/undefined/empty', () => {
      expect(extractResultSummary([null, undefined, '', {}, []])).toBe(null);
    });

    // Edge cases
    it('handles very long strings', () => {
      const longString = 'a'.repeat(1000);
      expect(extractResultSummary(longString)).toBe(longString);
    });

    it('handles strings with only ANSI codes', () => {
      expect(extractResultSummary('\x1B[31m\x1B[32m\x1B[33m')).toBe(null);
    });

    it('returns JSON string as-is (not parsed)', () => {
      // extractResultSummary treats JSON strings as regular strings, not as objects
      const jsonStr = '{"summary": "found"}';
      expect(extractResultSummary(jsonStr)).toBe(jsonStr);
    });
  });

  describe('getSubagentResultPreview', () => {
    it('returns undefined for undefined input', () => {
      expect(getSubagentResultPreview(undefined)).toBe(undefined);
    });

    it('returns undefined for null input', () => {
      expect(getSubagentResultPreview(null)).toBe(undefined);
    });

    it('returns undefined for empty string', () => {
      expect(getSubagentResultPreview('')).toBe(undefined);
    });

    it('returns undefined for whitespace-only string', () => {
      expect(getSubagentResultPreview('   \n\t  ')).toBe(undefined);
    });

    it('returns truncated plain text for non-JSON input', () => {
      const longText = `${'a'.repeat(300)} end`;
      const result = getSubagentResultPreview(longText);
      expect(result).toContain('a');
      expect(result).toContain('...');
      expect(result.length).toBeLessThanOrEqual(222); // maxLength-1 + "..." = 221 chars
    });

    it('parses JSON and extracts summary from known result keys', () => {
      expect(getSubagentResultPreview(JSON.stringify({ summary: 'test summary' }))).toBe('test summary');
    });

    it('handles JSON with ANSI codes', () => {
      const ansiJson = JSON.stringify({ summary: '\x1B[31mcolored\x1B[0m' });
      const result = getSubagentResultPreview(ansiJson);
      expect(result).toBe('colored');
    });

    it('falls back to formatted JSON when no summary found', () => {
      // Use an object with no extractable values
      const jsonWithoutSummary = JSON.stringify({});
      const result = getSubagentResultPreview(jsonWithoutSummary);
      // formatToolDetail pretty-prints the JSON, then it's trimmed
      expect(result).toBe('{}');
      expect(result.length).toBeLessThanOrEqual(222);
    });

    it('handles arrays in JSON', () => {
      const result = getSubagentResultPreview(JSON.stringify([1, 2, 3]));
      // extractResultSummary returns the first non-null summary from the array
      // For arrays of primitives, it returns the first element as a string
      expect(result).toBe('1');
    });

    it('truncates long results to 220 chars', () => {
      const longSummary = 'a'.repeat(300);
      const result = getSubagentResultPreview(JSON.stringify({ summary: longSummary }));
      expect(result.length).toBeLessThanOrEqual(222);
      expect(result).toContain('...');
    });

    it('handles invalid JSON gracefully', () => {
      const result = getSubagentResultPreview('not valid json {');
      expect(result).toBeDefined();
      expect(result).not.toBeUndefined();
    });

    it('handles JSON with nested objects', () => {
      const result = getSubagentResultPreview(JSON.stringify({ outer: { summary: 'nested' } }));
      expect(result).toBe('nested');
    });

    it('handles JSON with multiple priority keys (returns first found)', () => {
      const json = JSON.stringify({ result: 'result', summary: 'summary' });
      const result = getSubagentResultPreview(json);
      expect(result).toBe('summary'); // "summary" comes before "result" in priority list
    });

    it('handles special characters in JSON', () => {
      const json = JSON.stringify({ content: 'hello <world> & test' });
      const result = getSubagentResultPreview(json);
      expect(result).toContain('hello');
      expect(result).toContain('world');
    });
  });
});
