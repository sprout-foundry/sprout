import { afterEach, describe, expect, it } from 'vitest';
import {
  AUTO_HOST,
  CLOUD_HOST,
  defaultRunHost,
  escalationHostKey,
  getRememberedHost,
  rememberHost,
  toTxnHostChoice,
} from './escalationHost';
import type { Runner } from './runners';

const REPO = 'https://github.com/acme/app';

const runner = (id: string, status: string): Runner => ({ runner_id: id, name: `n-${id}`, status, mode: 'container' });

afterEach(() => {
  window.localStorage.clear();
});

describe('remembered host', () => {
  it('round-trips per repo and ignores "auto"', () => {
    expect(getRememberedHost(REPO)).toBeNull();
    rememberHost(REPO, { kind: 'runner', runnerId: 'r-1', name: 'MacBook' });
    expect(getRememberedHost(REPO)).toEqual({ kind: 'runner', runnerId: 'r-1', name: 'MacBook' });
    expect(window.localStorage.getItem(escalationHostKey(REPO))).not.toBeNull();
    expect(getRememberedHost('https://github.com/acme/other')).toBeNull();

    rememberHost(REPO, CLOUD_HOST);
    expect(getRememberedHost(REPO)).toEqual(CLOUD_HOST);
    rememberHost(REPO, AUTO_HOST);
    expect(getRememberedHost(REPO)).toEqual(CLOUD_HOST);
  });

  it('treats a corrupt entry as nothing remembered', () => {
    window.localStorage.setItem(escalationHostKey(REPO), '{not json');
    expect(getRememberedHost(REPO)).toBeNull();
    window.localStorage.setItem(escalationHostKey(REPO), JSON.stringify({ kind: 'runner' }));
    expect(getRememberedHost(REPO)).toBeNull();
  });
});

describe('defaultRunHost', () => {
  it('prefers an online runner, else the cloud', () => {
    expect(defaultRunHost(REPO, [])).toEqual(CLOUD_HOST);
    expect(defaultRunHost(REPO, [runner('a', 'offline'), runner('b', 'busy')])).toEqual(CLOUD_HOST);
    expect(defaultRunHost(REPO, [runner('a', 'offline'), runner('b', 'online')])).toEqual({
      kind: 'runner',
      runnerId: 'b',
      name: 'n-b',
    });
  });

  it('uses the remembered choice when it is still available', () => {
    const runners = [runner('a', 'online'), runner('b', 'busy')];
    rememberHost(REPO, CLOUD_HOST);
    expect(defaultRunHost(REPO, runners)).toEqual(CLOUD_HOST);
    rememberHost(REPO, { kind: 'runner', runnerId: 'b', name: 'old name' });
    expect(defaultRunHost(REPO, runners)).toEqual({ kind: 'runner', runnerId: 'b', name: 'n-b' });
  });

  it('falls back when the remembered runner is offline or gone', () => {
    rememberHost(REPO, { kind: 'runner', runnerId: 'gone', name: 'x' });
    expect(defaultRunHost(REPO, [runner('a', 'online')])).toEqual({ kind: 'runner', runnerId: 'a', name: 'n-a' });
    expect(defaultRunHost(REPO, [])).toEqual(CLOUD_HOST);
  });

  it('starts on the cloud after a runner turned the run down', () => {
    expect(defaultRunHost(REPO, [runner('a', 'online'), runner('b', 'online')], 'a')).toEqual(CLOUD_HOST);
  });
});

describe('toTxnHostChoice', () => {
  it('maps the UI choice to the platform host selector', () => {
    expect(toTxnHostChoice(AUTO_HOST)).toEqual({ host: 'auto' });
    expect(toTxnHostChoice(CLOUD_HOST)).toEqual({ host: 'fly' });
    expect(toTxnHostChoice({ kind: 'runner', runnerId: 'r-1', name: 'm' })).toEqual({
      host: 'runner',
      runnerId: 'r-1',
    });
  });
});
