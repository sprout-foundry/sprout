import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchPlatformGitHubConnected, listPlatformRepos } from './platformGitHub';

afterEach(() => vi.unstubAllGlobals());

function stubFetch(...bodies: unknown[]) {
  const fetchMock = vi.fn();
  for (const body of bodies) {
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify(body), { status: 200 }));
  }
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('platformGitHub', () => {
  it("reads the account's GitHub connection", async () => {
    stubFetch({ github_connected: true });
    await expect(fetchPlatformGitHubConnected()).resolves.toBe(true);
    stubFetch({});
    await expect(fetchPlatformGitHubConnected()).resolves.toBe(false);
  });

  it('lists repositories across pages as GitHub repos', async () => {
    const repo = (id: number, name: string) => ({
      id,
      full_name: `acme/${name}`,
      html_url: `https://github.com/acme/${name}`,
      default_branch: 'main',
      private: id === 2,
      updated_at: '2026-09-01T00:00:00Z',
    });
    const fetchMock = stubFetch(
      { repos: [repo(1, 'one')], next_cursor: 'c1' },
      { repos: [repo(2, 'two')], next_cursor: null },
    );
    const repos = await listPlatformRepos();
    expect(repos.map((r) => r.full_name)).toEqual(['acme/one', 'acme/two']);
    expect(repos[1]).toMatchObject({
      name: 'two',
      private: true,
      clone_url: 'https://github.com/acme/two.git',
      owner: { login: 'acme' },
    });
    expect(fetchMock.mock.calls[1][0]).toContain('cursor=c1');
  });

  it('surfaces a failed listing', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 429 })));
    await expect(listPlatformRepos()).rejects.toThrow('HTTP 429');
  });
});
