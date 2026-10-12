import { getActiveHost } from '../host/accessor';
import { NATIVE_FS_ENABLED } from './nativeFsStubs/nativeFsFlag';
import { NATIVE_GIT_ENABLED } from './nativeGitStubs/nativeGitFlag';

/**
 * isomorphic-git `corsProxy` for in-browser git. Git hosts' smart-HTTP
 * endpoints send no CORS headers, so a page can't clone, pull or push against
 * them directly; a host that runs the agent in the browser forwards git
 * smart-HTTP through a proxy of its own. The host names it with
 * `transport.gitProxyURL`; a `bearer` host that names none is the hosted
 * platform, which serves it at /git-proxy (same origin, so the session cookie
 * authenticates it and the platform can add the user's connected git-host
 * credential). Native shells run git themselves or don't enforce CORS, so
 * they get no proxy.
 */
export function gitCorsProxy(): string | undefined {
  if (NATIVE_FS_ENABLED || NATIVE_GIT_ENABLED || typeof window === 'undefined') return undefined;
  const transport = getActiveHost()?.transport;
  if (!transport) return undefined;
  if (transport.gitProxyURL !== undefined) return resolveProxyURL(transport.gitProxyURL);
  if (transport.authMode !== 'bearer') return undefined;
  return `${window.location.origin}/git-proxy`;
}

function resolveProxyURL(raw: string): string | undefined {
  const trimmed = raw.trim();
  if (!trimmed) return undefined;
  try {
    return new URL(trimmed, window.location.origin).toString().replace(/\/+$/, '');
  } catch {
    return undefined;
  }
}
