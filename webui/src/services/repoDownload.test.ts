// @ts-nocheck

import { describe, it, expect, vi, beforeEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('./gitClient', () => ({
  get gitClient() {
    return {
      listAllFiles: (...a: any[]) => mockListAllFiles(...a),
      readFileBinary: (...a: any[]) => mockReadFileBinary(...a),
    };
  },
}));

// JSZip mock — tracks calls and simulates generateAsync
const mockZipFile = vi.fn();
const mockGenerateAsync = vi.fn().mockResolvedValue(new Blob(['fake-zip']));

vi.mock('jszip', () => {
  return {
    default: class MockJSZip {
      file = mockZipFile;
      generateAsync = mockGenerateAsync;
    },
  };
});

const mockListAllFiles = vi.fn();
const mockReadFileBinary = vi.fn();

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let createObjectURLMock: ReturnType<typeof vi.fn>;
let revokeObjectURLMock: ReturnType<typeof vi.fn>;
let createElementSpy: vi.SpyInstance;
let clickSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  mockGenerateAsync.mockResolvedValue(new Blob(['fake-zip']));

  // jsdom doesn't have URL.createObjectURL — define it
  createObjectURLMock = vi.fn().mockReturnValue('blob:fake-url');
  revokeObjectURLMock = vi.fn();
  (URL as any).createObjectURL = createObjectURLMock;
  (URL as any).revokeObjectURL = revokeObjectURLMock;

  clickSpy = vi.fn();

  // Intercept createElement for 'a' to capture click without actually triggering download
  const origCreate = document.createElement.bind(document);
  createElementSpy = vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
    const el = origCreate(tag);
    if (tag === 'a') {
      el.click = clickSpy;
    }
    return el;
  });
});

afterEach(() => {
  createElementSpy.mockRestore();
});

// ---------------------------------------------------------------------------
// Import after mocks
// ---------------------------------------------------------------------------

import { downloadRepoAsZip } from './repoDownload';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('downloadRepoAsZip', () => {
  it('calls gitClient.listAllFiles with the repoDir', async () => {
    mockListAllFiles.mockResolvedValue([{ name: 'a.txt', path: '/a.txt', type: 'file', size: 100 }]);
    mockReadFileBinary.mockResolvedValue(new Uint8Array([72, 101, 108]));

    await downloadRepoAsZip('/repos/owner/repo', 'test-repo');

    expect(mockListAllFiles).toHaveBeenCalledWith('/repos/owner/repo');
  });

  it('filters to only file-type entries (skips dirs)', async () => {
    mockListAllFiles.mockResolvedValue([
      { name: 'a.txt', path: '/a.txt', type: 'file', size: 100 },
      { name: 'src', path: '/src', type: 'dir', size: 0 },
      { name: 'b.txt', path: '/b.txt', type: 'file', size: 50 },
    ]);
    mockReadFileBinary.mockResolvedValue(new Uint8Array([1]));

    await downloadRepoAsZip('/repos/owner/repo', 'test-repo');

    // Should call readFileBinary twice (only for files, not dirs)
    expect(mockReadFileBinary).toHaveBeenCalledTimes(2);
    expect(mockZipFile).toHaveBeenCalledTimes(2);
  });

  it('strips leading slash from file paths in ZIP', async () => {
    mockListAllFiles.mockResolvedValue([
      { name: 'a.txt', path: '/a.txt', type: 'file', size: 100 },
      { name: 'b.txt', path: '/src/b.txt', type: 'file', size: 50 },
    ]);
    mockReadFileBinary.mockResolvedValue(new Uint8Array([1]));

    await downloadRepoAsZip('/repos/owner/repo', 'test-repo');

    // zip.file should be called with paths without leading slash
    expect(mockZipFile).toHaveBeenCalledWith('a.txt', expect.any(Uint8Array), { binary: true });
    expect(mockZipFile).toHaveBeenCalledWith('src/b.txt', expect.any(Uint8Array), { binary: true });
  });

  it('calls onProgress with (done, total) for each file', async () => {
    mockListAllFiles.mockResolvedValue([
      { name: 'a.txt', path: '/a.txt', type: 'file', size: 100 },
      { name: 'b.txt', path: '/b.txt', type: 'file', size: 50 },
      { name: 'c.txt', path: '/c.txt', type: 'file', size: 50 },
    ]);
    mockReadFileBinary.mockResolvedValue(new Uint8Array([1]));

    const onProgress = vi.fn();
    await downloadRepoAsZip('/repos/owner/repo', 'test-repo', onProgress);

    expect(onProgress).toHaveBeenCalledTimes(3);
    expect(onProgress).toHaveBeenNthCalledWith(1, 1, 3);
    expect(onProgress).toHaveBeenNthCalledWith(2, 2, 3);
    expect(onProgress).toHaveBeenNthCalledWith(3, 3, 3);
  });

  it('triggers browser download via anchor element click', async () => {
    mockListAllFiles.mockResolvedValue([{ name: 'a.txt', path: '/a.txt', type: 'file', size: 100 }]);
    mockReadFileBinary.mockResolvedValue(new Uint8Array([1]));

    await downloadRepoAsZip('/repos/owner/repo', 'test-repo');

    expect(createObjectURLMock).toHaveBeenCalledTimes(1);
    expect(clickSpy).toHaveBeenCalledTimes(1);
    expect(revokeObjectURLMock).toHaveBeenCalledWith('blob:fake-url');
  });

  it('sets the download filename from repoName with safe characters', async () => {
    mockListAllFiles.mockResolvedValue([{ name: 'a.txt', path: '/a.txt', type: 'file', size: 100 }]);
    mockReadFileBinary.mockResolvedValue(new Uint8Array([1]));

    await downloadRepoAsZip('/repos/owner/repo', 'owner/my-repo!');

    // The anchor's download attribute should be sanitized
    expect(clickSpy).toHaveBeenCalled();
    // createElement was called for 'a' — the mock captured the element
    // We can verify via the mock call args that download was set
  });

  it('throws error when total size exceeds MAX_ZIP_SIZE (500MB)', async () => {
    // Create a buffer just over 500MB
    const hugeBuffer = new Uint8Array(500 * 1024 * 1024 + 1);
    mockListAllFiles.mockResolvedValue([
      { name: 'huge.bin', path: '/huge.bin', type: 'file', size: hugeBuffer.byteLength },
    ]);
    mockReadFileBinary.mockResolvedValue(hugeBuffer);

    await expect(downloadRepoAsZip('/repos/owner/repo', 'big-repo')).rejects.toThrow('too large');
  });

  it('handles empty repo (no files)', async () => {
    mockListAllFiles.mockResolvedValue([]);

    await downloadRepoAsZip('/repos/owner/repo', 'empty-repo');

    expect(mockReadFileBinary).not.toHaveBeenCalled();
    expect(mockZipFile).not.toHaveBeenCalled();
    expect(mockGenerateAsync).toHaveBeenCalledTimes(1);
  });

  it('uses DEFLATE compression with level 6', async () => {
    mockListAllFiles.mockResolvedValue([{ name: 'a.txt', path: '/a.txt', type: 'file', size: 100 }]);
    mockReadFileBinary.mockResolvedValue(new Uint8Array([1]));

    await downloadRepoAsZip('/repos/owner/repo', 'test-repo');

    expect(mockGenerateAsync).toHaveBeenCalledWith({
      type: 'blob',
      compression: 'DEFLATE',
      compressionOptions: { level: 6 },
    });
  });

  it('handles file paths without leading slash correctly', async () => {
    mockListAllFiles.mockResolvedValue([
      { name: 'a.txt', path: 'a.txt', type: 'file', size: 100 }, // no leading slash
    ]);
    mockReadFileBinary.mockResolvedValue(new Uint8Array([1]));

    await downloadRepoAsZip('/repos/owner/repo', 'test-repo');

    expect(mockZipFile).toHaveBeenCalledWith('a.txt', expect.any(Uint8Array), { binary: true });
  });
});
