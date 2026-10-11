import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const activeHostRef = vi.hoisted(() => ({ value: null as unknown }));
vi.mock('../host/accessor', () => ({
  getActiveHost: () => activeHostRef.value,
}));
vi.mock('./apiAdapter', () => ({
  getAdapter: vi.fn(() => null),
}));

import { DEFAULT_EXPORT_FILENAME, downloadExport, filenameFromContentDisposition } from './exportDownload';

describe('filenameFromContentDisposition', () => {
  it.each([
    [null, DEFAULT_EXPORT_FILENAME],
    ['attachment', DEFAULT_EXPORT_FILENAME],
    ['attachment; filename="chat.md"', 'chat.md'],
    ['attachment; filename=chat.md', 'chat.md'],
    [`attachment; filename="x.md"; filename*=UTF-8''r%C3%A9sum%C3%A9.md`, 'résumé.md'],
    [`attachment; filename="fallback.md"; filename*=UTF-8''%E0%A4%A`, 'fallback.md'],
  ])('%s -> %s', (header, expected) => {
    expect(filenameFromContentDisposition(header)).toBe(expected);
  });
});

describe('downloadExport', () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  let clicked: HTMLAnchorElement[];
  const originalCreate = URL.createObjectURL;
  const originalRevoke = URL.revokeObjectURL;

  beforeEach(() => {
    window.sessionStorage.setItem('sprout.webuiClientId', 'client-test');
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    clicked = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clicked.push(this);
    });
    URL.createObjectURL = vi.fn(() => 'blob:export-1');
    URL.revokeObjectURL = vi.fn();
  });

  afterEach(() => {
    activeHostRef.value = null;
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    URL.createObjectURL = originalCreate;
    URL.revokeObjectURL = originalRevoke;
  });

  it('fetches through the host transport and saves the body under the server filename', async () => {
    activeHostRef.value = { transport: { apiBaseURL: 'https://api.host.test', wsURL: '', authMode: 'none' } };
    fetchMock.mockResolvedValue(
      new Response('# chat', { status: 200, headers: { 'Content-Disposition': 'attachment; filename="chat.md"' } }),
    );

    const result = await downloadExport('/api/sessions/s1/export?format=markdown');

    expect(result).toEqual({ ok: true, filename: 'chat.md' });
    expect(fetchMock.mock.calls[0][0]).toBe('https://api.host.test/api/sessions/s1/export?format=markdown');
    expect(clicked).toHaveLength(1);
    expect(clicked[0].href).toBe('blob:export-1');
    expect(clicked[0].download).toBe('chat.md');
    expect(document.body.contains(clicked[0])).toBe(false);
  });

  it('reports a non-2xx status without starting a download', async () => {
    fetchMock.mockResolvedValue(new Response('{}', { status: 404 }));
    expect(await downloadExport('/api/sessions/missing/export')).toEqual({ ok: false, status: 404 });
    expect(fetchMock.mock.calls[0][0]).toBe('/api/sessions/missing/export');
    expect(clicked).toHaveLength(0);
  });

  it('rejects on a network failure', async () => {
    fetchMock.mockRejectedValue(new TypeError('Failed to fetch'));
    await expect(downloadExport('/api/sessions/s1/export')).rejects.toThrow('Failed to fetch');
  });
});
