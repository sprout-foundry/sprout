import { ansiToHtml, hasAnsiCodes, stripAnsiCodes } from './ansi';

describe('ansi utils', () => {
  test('strips basic SGR sequences', () => {
    const input = '\x1b[31mred\x1b[0m';
    expect(stripAnsiCodes(input)).toBe('red');
  });

  test('strips private CSI sequences like bracketed paste mode', () => {
    const input = '\x1b[?2004h$ pwd\x1b[?2004l';
    expect(stripAnsiCodes(input)).toBe('$ pwd');
  });

  test('strips OSC sequences', () => {
    const input = '\x1b]0;window title\x07hello';
    expect(stripAnsiCodes(input)).toBe('hello');
  });

  test('detects ansi/control sequences', () => {
    expect(hasAnsiCodes('\x1b[32mok\x1b[0m')).toBe(true);
    expect(hasAnsiCodes('plain text')).toBe(false);
  });

  test('converts SGR to HTML classes without leaking CSI fragments', () => {
    const input = '\x1b[01;34mblue\x1b[0m';
    const html = ansiToHtml(input);
    expect(html).toContain('ansi-blue');
    expect(html).toContain('blue');
    expect(html).not.toContain('[01;34m');
    expect(html).not.toContain('[0m');
  });

  test('strips non-CSI ESC control sequences like ESC=', () => {
    const input = '\x1b=hello';
    expect(stripAnsiCodes(input)).toBe('hello');
    const html = ansiToHtml(input);
    expect(html).toBe('hello');
  });

  test('handles 8-bit C1 CSI sequences', () => {
    const input = '\u009b31mred\u009b0m';
    const html = ansiToHtml(input);
    expect(html).toContain('ansi-red');
    expect(html).toContain('red');
  });
});
