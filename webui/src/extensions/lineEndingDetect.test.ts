import { detectLineEnding } from '@sprout/ui';

describe('detectLineEnding', () => {
  it('returns LF for empty string', () => {
    expect(detectLineEnding('').lineEnding).toBe('LF');
  });

  it('returns LF for Unix-style line endings', () => {
    const content = 'line1\nline2\nline3\n';
    expect(detectLineEnding(content).lineEnding).toBe('LF');
  });

  it('returns CRLF for Windows-style line endings', () => {
    const content = 'line1\r\nline2\r\nline3\r\n';
    expect(detectLineEnding(content).lineEnding).toBe('CRLF');
  });

  it('returns Mixed when both CRLF and bare LF are present', () => {
    // Mixed: some LF, some CRLF
    const content = 'line1\nline2\r\nline3\n';
    expect(detectLineEnding(content).lineEnding).toBe('Mixed');
  });

  it('returns LF when only CR is present (no CRLF)', () => {
    // Classic Mac OS line ending (just \r) — not \r\n, so detected as LF
    const content = 'line1\rline2\rline3\r';
    expect(detectLineEnding(content).lineEnding).toBe('LF');
  });

  it('returns LF for content with no line endings', () => {
    const content = 'just a single line';
    expect(detectLineEnding(content).lineEnding).toBe('LF');
  });

  it('detects CRLF in large content by scanning first 5000 chars', () => {
    // Both CRLF and bare LF are within first 5000 chars
    const content = 'line1\r\n' + 'a\n' + 'b'.repeat(6000);
    expect(detectLineEnding(content).lineEnding).toBe('Mixed');
  });

  it('returns LF when CRLF is beyond the scan window', () => {
    // Create content where CRLF is after the 5000-char scan window
    const padding = 'a'.repeat(6000);
    const content = padding + '\r\nmore content';
    // Only first 5000 chars are scanned, so CRLF won't be found
    expect(detectLineEnding(content).lineEnding).toBe('LF');
  });

  it('returns CRLF for CRLF at the very start', () => {
    const content = '\r\nhello';
    expect(detectLineEnding(content).lineEnding).toBe('CRLF');
  });

  it('returns LF for content with only LF at the very start', () => {
    const content = '\nhello';
    expect(detectLineEnding(content).lineEnding).toBe('LF');
  });

  it('returns Mixed when CRLF appears after bare LF', () => {
    const content = 'first\nsecond\r\nthird\n';
    expect(detectLineEnding(content).lineEnding).toBe('Mixed');
  });

  it('returns CRLF when all line breaks are CRLF (no bare LF)', () => {
    const content = 'line1\r\nline2\r\nline3\r\n';
    expect(detectLineEnding(content).lineEnding).toBe('CRLF');
  });

  it('handles null-like input gracefully', () => {
    // The function handles falsy values by returning LF
    expect(detectLineEnding('' as string).lineEnding).toBe('LF');
  });
});