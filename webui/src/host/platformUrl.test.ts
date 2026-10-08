import { afterEach, describe, expect, it } from 'vitest';
import { setActiveHost } from './accessor';
import { headlessHost } from './HostProvider';
import { githubRepoSlug } from './repoName';
import { platformHref, repoHubPath } from './platformUrl';
import type { HostTransport } from './types';

afterEach(() => {
  // Clear the active host so a test that set one cannot leak into the next.
  setActiveHost(headlessHost());
});

/** A transport with an optional platform base URL. */
function transport(platformURL?: string): HostTransport {
  return { apiBaseURL: '', wsURL: '', authMode: 'none', platformURL };
}

// SP-016 P0.3: platformHref must build absolute account-surface exit URLs
// when the host's transport carries the platform base, and fall back to the
// relative path — today's behavior — when it does not (local mode, or a host
// that did not provide one). The base is host-provided data: the builder reads
// the transport, never the bootstrap fetch.
describe('platformHref (SP-016 P0.3)', () => {
  it('returns the path verbatim when no platform URL is known', () => {
    expect(platformHref('/?from=editor', transport())).toBe('/?from=editor');
    expect(platformHref('/tasks/abc-123', transport())).toBe('/tasks/abc-123');
  });

  it('treats an empty-string platform URL as absent', () => {
    expect(platformHref('/?from=editor', transport(''))).toBe('/?from=editor');
  });

  it('builds an absolute URL when a platform URL is known', () => {
    const t = transport('https://platform.sprout.dev');
    expect(platformHref('/?from=editor', t)).toBe('https://platform.sprout.dev/?from=editor');
    expect(platformHref('/tasks/abc-123', t)).toBe('https://platform.sprout.dev/tasks/abc-123');
  });

  it('strips trailing slashes from the base before appending', () => {
    expect(platformHref('/tasks/abc-123', transport('https://platform.sprout.dev/'))).toBe(
      'https://platform.sprout.dev/tasks/abc-123',
    );
  });

  it('normalizes a path without a leading slash', () => {
    expect(platformHref('tasks/abc-123', transport('https://platform.sprout.dev'))).toBe(
      'https://platform.sprout.dev/tasks/abc-123',
    );
  });

  it("reads the active host's transport when none is passed (the React-free path)", () => {
    const host = headlessHost();
    host.transport.platformURL = 'https://active.sprout.dev';
    setActiveHost(host);
    expect(platformHref('/tasks/abc-123')).toBe('https://active.sprout.dev/tasks/abc-123');
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
