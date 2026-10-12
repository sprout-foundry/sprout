import { afterEach, describe, expect, it } from 'vitest';
import { setActiveHost } from '../host/accessor';
import { headlessHost } from '../host/HostProvider';
import type { HostTransport } from '../host/types';
import { gitCorsProxy } from './gitCorsProxy';

function withTransport(transport: HostTransport): void {
  setActiveHost({ ...headlessHost(), transport });
}

const base: HostTransport = { apiBaseURL: '', wsURL: '', authMode: 'none' };

describe('gitCorsProxy', () => {
  afterEach(() => setActiveHost(headlessHost()));

  it('defaults a bearer host to the platform proxy at the page origin', () => {
    withTransport({ ...base, authMode: 'bearer' });
    expect(gitCorsProxy()).toBe(`${window.location.origin}/git-proxy`);
  });

  it('gives a host without a declared proxy none', () => {
    withTransport({ ...base, agent: { kind: 'wasm', modelEndpoint: '' } });
    expect(gitCorsProxy()).toBeUndefined();
  });

  it('uses the proxy a host declares, resolving a path against the page origin', () => {
    withTransport({ ...base, gitProxyURL: '/my/git-proxy/' });
    expect(gitCorsProxy()).toBe(`${window.location.origin}/my/git-proxy`);
    withTransport({ ...base, gitProxyURL: 'https://git.example.test/proxy' });
    expect(gitCorsProxy()).toBe('https://git.example.test/proxy');
  });

  it("lets a bearer host declare that it serves no proxy with ''", () => {
    withTransport({ ...base, authMode: 'bearer', gitProxyURL: '' });
    expect(gitCorsProxy()).toBeUndefined();
  });
});
