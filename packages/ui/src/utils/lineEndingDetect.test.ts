import { detectLineEnding } from './lineEndingDetect';

describe('detectLineEnding', () => {
  describe('CRLF detection', () => {
    it('detects \\r\\n (CRLF) line endings', () => {
      const content = 'line1\r\nline2\r\nline3';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('CRLF');
    });

    it('detects CRLF with single line', () => {
      const content = 'line1\r\n';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('CRLF');
    });

    it('detects Mixed when CRLF at the beginning of file followed by LF', () => {
      const content = '\r\nline2\nline3';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('Mixed');
    });

    it('detects Mixed when CRLF in middle of file followed by LF', () => {
      const content = 'line1\r\nline2\nline3';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('Mixed');
    });
  });

  describe('LF detection', () => {
    it('detects \\n (LF) line endings', () => {
      const content = 'line1\nline2\nline3';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('LF');
    });

    it('detects LF with single line', () => {
      const content = 'line1\n';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('LF');
    });

    it('detects LF for Unix-style files', () => {
      const content = '#!/bin/bash\necho "Hello"';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('LF');
    });
  });

  describe('CR detection', () => {
    it('detects lone \\r (CR) as LF by default', () => {
      const content = 'line1\rline2\rline3';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('LF');
    });

    it('handles old Mac-style CR endings', () => {
      const content = 'line1\rline2\rline3\r';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('LF');
    });
  });

  describe('Empty and special cases', () => {
    it('returns LF default for empty string', () => {
      const content = '';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('LF');
    });

    it('returns LF default for null/undefined', () => {
      const result1 = detectLineEnding(null as unknown as string);
      const result2 = detectLineEnding(undefined as unknown as string);
      expect(result1.lineEnding).toBe('LF');
      expect(result2.lineEnding).toBe('LF');
    });

    it('returns LF for single line without line ending', () => {
      const content = 'single line';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('LF');
    });
  });

  describe('Mixed line endings', () => {
    it('detects Mixed when both CRLF and bare LF are present', () => {
      const content = 'line1\r\nline2\nline3\r\nline4';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('Mixed');
    });

    it('detects Mixed with CRLF first then LF', () => {
      const content = 'line1\r\nline2\n';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('Mixed');
    });

    it('detects Mixed with LF first then CRLF', () => {
      const content = 'line1\nline2\r\n';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('Mixed');
    });

    it('handles complex mixed endings', () => {
      const content = 'line1\r\nline2\nline3\rline4\r\nline5';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('Mixed');
    });
  });

  describe('Performance optimization', () => {
    it('only scans first 5000 characters for long files', () => {
      const longContent = 'line1\nline2\nline3\n'.repeat(2000); // ~60k chars
      const result = detectLineEnding(longContent);
      expect(result.lineEnding).toBe('LF');
    });

    it('detects Mixed when CRLF in prefix and LF in suffix', () => {
      const prefix = 'line1\r\nline2\r\nline3\r\n';
      const suffix = '\n'.repeat(10000);
      const result = detectLineEnding(prefix + suffix);
      // This is Mixed because after the prefix, the suffix has bare LF
      expect(result.lineEnding).toBe('Mixed');
    });

    it('detects CRLF when only CRLF in first 5000 chars', () => {
      const prefix = 'line1\r\nline2\r\nline3\r\n';
      const suffix = ' '.repeat(10000); // spaces instead of \n
      const result = detectLineEnding(prefix + suffix);
      expect(result.lineEnding).toBe('CRLF');
    });
  });

  describe('Real-world examples', () => {
    it('handles JavaScript file with LF', () => {
      const content = 'const x = 5;\nconsole.log(x);\n';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('LF');
    });

    it('handles Windows batch file with CRLF', () => {
      const content = '@echo off\r\necho Hello\r\npause\r\n';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('CRLF');
    });

    it('handles JSON file with no trailing newline', () => {
      const content = '{"key": "value"}';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('LF');
    });

    it('handles file with multiple consecutive newlines', () => {
      const content = 'line1\n\n\nline2\n';
      const result = detectLineEnding(content);
      expect(result.lineEnding).toBe('LF');
    });
  });

  describe('Result structure', () => {
    it('returns object with lineEnding property', () => {
      const result = detectLineEnding('test\n');
      expect(result).toHaveProperty('lineEnding');
      expect(typeof result.lineEnding).toBe('string');
    });

    it('returns valid LineEnding type values', () => {
      const validValues = ['LF', 'CRLF', 'Mixed'] as const;
      const results = [
        detectLineEnding('test\n'),
        detectLineEnding('test\r\n'),
        detectLineEnding('test\nline\r\n'),
      ];
      results.forEach(r => {
        expect(validValues).toContain(r.lineEnding);
      });
    });
  });
});
