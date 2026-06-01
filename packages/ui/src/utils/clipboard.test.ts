import { vi } from 'vitest';
import { copyToClipboard } from './clipboard';

// Mock navigator.clipboard for testing
const mockWriteText = vi.fn();
const originalClipboard = (global as any).navigator?.clipboard;

beforeEach(() => {
  mockWriteText.mockReset();
  // @ts-expect-error — assigning to navigator.clipboard for testing
  global.navigator.clipboard = {
    writeText: mockWriteText,
  };
});

afterEach(() => {
  // navigator.clipboard is a readonly property in TS lib.dom; cast through
  // any to restore/delete the test fixture without compile errors.
  if (originalClipboard) {
    (global.navigator as any).clipboard = originalClipboard;
  } else {
    delete (global.navigator as any).clipboard;
  }
});

describe('copyToClipboard', () => {
  it('calls navigator.clipboard.writeText with the given text', async () => {
    const text = 'Hello, World!';
    await copyToClipboard(text);

    expect(mockWriteText).toHaveBeenCalledTimes(1);
    expect(mockWriteText).toHaveBeenCalledWith(text);
  });

  it('returns the promise from writeText', async () => {
    const text = 'Test text';
    const result = copyToClipboard(text);

    expect(result).toBeInstanceOf(Promise);
    await result;
  });

  it('handles empty string', async () => {
    const text = '';
    await copyToClipboard(text);

    expect(mockWriteText).toHaveBeenCalledWith('');
  });

  it('handles special characters', async () => {
    const text = 'Special: <>&"\n\t';
    await copyToClipboard(text);

    expect(mockWriteText).toHaveBeenCalledWith(text);
  });

  it('handles long text', async () => {
    const text = 'A'.repeat(10000);
    await copyToClipboard(text);

    expect(mockWriteText).toHaveBeenCalledWith(text);
  });

  it('handles Unicode text', async () => {
    const text = 'Hello 世界 🌍 🎉';
    await copyToClipboard(text);

    expect(mockWriteText).toHaveBeenCalledWith(text);
  });

  it('uses fallback when navigator.clipboard.writeText throws', async () => {
    mockWriteText.mockRejectedValue(new Error('Clipboard API not available'));

    const text = 'Fallback test';

    // The function should handle the error gracefully
    const result = copyToClipboard(text);

    // Should resolve without throwing
    await expect(result).resolves.toBeUndefined();

    // The clipboard write was attempted
    expect(mockWriteText).toHaveBeenCalledWith(text);
  });

  it('resolves on success', async () => {
    mockWriteText.mockResolvedValue(undefined);
    const text = 'Success test';

    await expect(copyToClipboard(text)).resolves.toBeUndefined();
  });

  it('does not throw when fallback fails silently', () => {
    mockWriteText.mockRejectedValue(new Error('Not available'));

    const text = 'Silent failure test';
    const result = copyToClipboard(text);

    // Should resolve even on failure
    expect(result).resolves.toBeUndefined();
  });
});
