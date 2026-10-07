/**
 * Host-agnostic repository naming (host/repoName.ts): parse any git URL
 * (GitHub, GitLab, Bitbucket, scp- or https-style) into a display name. These
 * helpers carry no platform concept, so they are exported from the public host
 * entry and used by components everywhere (not only in a hosted build).
 */
import { describe, expect, it } from 'vitest';
import { githubRepoSlug, repoName, repoSlug } from './repoName';

describe('repoSlug / repoName', () => {
  it('names repos on any host, keeping GitLab subgroups', () => {
    expect(repoSlug('https://github.com/acme/app.git')).toBe('acme/app');
    expect(repoSlug('https://gitlab.com/group/sub/app/-/tree/main')).toBe('group/sub/app');
    expect(repoSlug('https://bitbucket.org/team/app')).toBe('team/app');
    expect(repoSlug('git@gitlab.com:group/sub/app.git')).toBe('group/sub/app');
    expect(repoName('https://gitlab.com/group/sub/app')).toBe('app');
    expect(repoSlug('not a url')).toBeNull();
    expect(repoSlug('https://gitlab.com/only')).toBeNull();
    expect(repoSlug(null)).toBeNull();
  });
});

describe('githubRepoSlug', () => {
  it('is "owner/name" for github.com only', () => {
    expect(githubRepoSlug('https://github.com/acme/widgets')).toBe('acme/widgets');
    expect(githubRepoSlug('git@github.com:acme/my.repo.git')).toBe('acme/my.repo');
    expect(githubRepoSlug('https://gitlab.com/acme/widgets')).toBeNull();
    expect(githubRepoSlug(undefined)).toBeNull();
  });
});
