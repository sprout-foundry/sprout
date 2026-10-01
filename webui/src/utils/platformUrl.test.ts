import { githubRepoSlug, platformHref, repoHubPath } from './platformUrl';
import { getPlatformURL } from '../bootstrapAdapter';

// Mock the bootstrap-adapter surface the helper reads — the real module
// kicks off a bootstrap fetch on import, which is irrelevant to the pure
// URL-building logic under test.
vi.mock('../bootstrapAdapter', () => ({
  getPlatformURL: vi.fn(),
}));

const mockedGetPlatformURL = vi.mocked(getPlatformURL);

// SP-016 P0.3: platformHref must build absolute account-surface exit URLs
// when the host knows the platform base (SPROUT_PLATFORM_URL / cloud
// bootstrap), and fall back to the relative path — today's behavior —
// when it does not (local mode, or a host that did not provide one).
describe('platformHref (SP-016 P0.3)', () => {
  it('returns the path verbatim when no platform URL is known', () => {
    mockedGetPlatformURL.mockReturnValue(undefined);
    expect(platformHref('/?from=editor')).toBe('/?from=editor');
    expect(platformHref('/tasks/abc-123')).toBe('/tasks/abc-123');
  });

  it('treats an empty-string platform URL as absent', () => {
    mockedGetPlatformURL.mockReturnValue('');
    expect(platformHref('/?from=editor')).toBe('/?from=editor');
  });

  it('builds an absolute URL when a platform URL is known', () => {
    mockedGetPlatformURL.mockReturnValue('https://platform.sprout.dev');
    expect(platformHref('/?from=editor')).toBe('https://platform.sprout.dev/?from=editor');
    expect(platformHref('/tasks/abc-123')).toBe('https://platform.sprout.dev/tasks/abc-123');
  });

  it('strips trailing slashes from the base before appending', () => {
    mockedGetPlatformURL.mockReturnValue('https://platform.sprout.dev/');
    expect(platformHref('/tasks/abc-123')).toBe('https://platform.sprout.dev/tasks/abc-123');
  });

  it('normalizes a path without a leading slash', () => {
    mockedGetPlatformURL.mockReturnValue('https://platform.sprout.dev');
    expect(platformHref('tasks/abc-123')).toBe('https://platform.sprout.dev/tasks/abc-123');
  });
});

describe('repoHubPath', () => {
  it('links a GitHub repo to its hub page in the SPA hash', () => {
    expect(repoHubPath('https://github.com/acme/widgets')).toBe('/?from=editor#/repos/acme/widgets');
    expect(repoHubPath('https://github.com/acme/widgets.git')).toBe('/?from=editor#/repos/acme/widgets');
    expect(repoHubPath('git@github.com:acme/my.repo.git')).toBe('/?from=editor#/repos/acme/my.repo');
  });

  it('falls back to the dashboard for unknown or non-GitHub repos', () => {
    expect(repoHubPath(undefined)).toBe('/?from=editor');
    expect(repoHubPath('https://gitlab.com/acme/widgets')).toBe('/?from=editor');
    expect(githubRepoSlug('https://github.com/acme')).toBeNull();
  });
});
