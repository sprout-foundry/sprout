import { describe, expect, it } from 'vitest';
import { terminalText } from './terminalText';

describe('terminalText', () => {
  it('prints output in the order it was written, errors in red', () => {
    const text = terminalText({
      stdout: 'x\n',
      stderr: 'command not found: node\n',
      exitCode: 0,
      output: [{ err: true, text: 'command not found: node\n' }, { text: 'x\n' }],
    });
    expect(text).toBe('\x1b[31mcommand not found: node\r\n\x1b[0mx\r\n');
  });

  it('falls back to stdout then stderr without an ordered transcript', () => {
    expect(terminalText({ stdout: 'a\n', stderr: 'b', exitCode: 1 })).toBe('a\r\n\x1b[31mb\x1b[0m');
    expect(terminalText({ stdout: '', stderr: '', exitCode: 0 })).toBe('');
  });
});
