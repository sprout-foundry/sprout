import { hasAnsiCodes, stripAnsiCodes } from './ansi';

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
});
