import { getStatusInfo } from './git';

describe('getStatusInfo', () => {
  describe('A (Added) status', () => {
    it('returns correct label and className for uppercase A', () => {
      const result = getStatusInfo('A');
      expect(result.label).toBe('A');
      expect(result.className).toBe('status-a');
    });

    it('returns correct label and className for lowercase a', () => {
      const result = getStatusInfo('a');
      expect(result.label).toBe('A');
      expect(result.className).toBe('status-a');
    });
  });

  describe('M (Modified) status', () => {
    it('returns correct label and className for uppercase M', () => {
      const result = getStatusInfo('M');
      expect(result.label).toBe('M');
      expect(result.className).toBe('status-m');
    });

    it('returns correct label and className for lowercase m', () => {
      const result = getStatusInfo('m');
      expect(result.label).toBe('M');
      expect(result.className).toBe('status-m');
    });
  });

  describe('D (Deleted) status', () => {
    it('returns correct label and className for uppercase D', () => {
      const result = getStatusInfo('D');
      expect(result.label).toBe('D');
      expect(result.className).toBe('status-d');
    });

    it('returns correct label and className for lowercase d', () => {
      const result = getStatusInfo('d');
      expect(result.label).toBe('D');
      expect(result.className).toBe('status-d');
    });
  });

  describe('R (Renamed) status', () => {
    it('returns correct label and className for uppercase R', () => {
      const result = getStatusInfo('R');
      expect(result.label).toBe('R');
      expect(result.className).toBe('status-r');
    });

    it('returns correct label and className for lowercase r', () => {
      const result = getStatusInfo('r');
      expect(result.label).toBe('R');
      expect(result.className).toBe('status-r');
    });
  });

  describe('C (Copied) status', () => {
    it('returns correct label and className for uppercase C', () => {
      const result = getStatusInfo('C');
      expect(result.label).toBe('C');
      expect(result.className).toBe('status-c');
    });

    it('returns correct label and className for lowercase c', () => {
      const result = getStatusInfo('c');
      expect(result.label).toBe('C');
      expect(result.className).toBe('status-c');
    });
  });

  describe('Unknown status codes', () => {
    it('returns the original status as label for unknown codes', () => {
      const result = getStatusInfo('X');
      expect(result.label).toBe('X');
      expect(result.className).toBe('status-unknown');
    });

    it('returns the original status as label for multi-char codes', () => {
      const result = getStatusInfo('AM');
      expect(result.label).toBe('A');
      expect(result.className).toBe('status-a');
    });

    it('returns the original status as label for special git codes', () => {
      const result = getStatusInfo('U');
      expect(result.label).toBe('U');
      expect(result.className).toBe('status-unknown');
    });

    it('handles empty string', () => {
      const result = getStatusInfo('');
      expect(result.label).toBe('?');
      expect(result.className).toBe('status-unknown');
    });

    it('handles ? (untracked)', () => {
      const result = getStatusInfo('?');
      expect(result.label).toBe('?');
      expect(result.className).toBe('status-unknown');
    });

    it('handles ! (ignored)', () => {
      const result = getStatusInfo('!');
      expect(result.label).toBe('!');
      expect(result.className).toBe('status-unknown');
    });
  });

  describe('Edge cases', () => {
    it('handles longer status strings', () => {
      const result = getStatusInfo('MM');
      expect(result.label).toBe('M');
      expect(result.className).toBe('status-m');
    });

    it('handles spaces in status', () => {
      const result = getStatusInfo(' M');
      // When no match, it returns the original status string, not just first char
      expect(result.label).toBe(' M');
      expect(result.className).toBe('status-unknown');
    });

    it('handles status with trailing characters', () => {
      const result = getStatusInfo('M ');
      expect(result.label).toBe('M');
      expect(result.className).toBe('status-m');
    });
  });
});
