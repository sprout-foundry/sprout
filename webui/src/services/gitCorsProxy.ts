import { getActiveHost } from '../host/accessor';
import { NATIVE_FS_ENABLED } from './nativeFsStubs/nativeFsFlag';
import { NATIVE_GIT_ENABLED } from './nativeGitStubs/nativeGitFlag';

/**
 * isomorphic-git `corsProxy` for the hosted browser IDE. GitHub's git
 * endpoints send no CORS headers, so a page can't clone, pull or push against
 * them directly; the platform serves the IDE and forwards git smart-HTTP at
 * /git-proxy (same origin, so the session cookie authenticates it and the
 * platform can add the user's connected GitHub token). Native shells run git
 * themselves or don't enforce CORS, so they get no proxy.
 *
 * host.8: the hosted (cloud) build is the one whose host transport
 * authenticates against a platform (authMode 'bearer'); the local build's
 * transport is unauthenticated ('none') and gets no proxy, matching the
 * former isCloud branch.
 */
export function gitCorsProxy(): string | undefined {
  const isHosted = getActiveHost()?.transport.authMode === 'bearer';
  if (!isHosted || NATIVE_FS_ENABLED || NATIVE_GIT_ENABLED || typeof window === 'undefined') return undefined;
  return `${window.location.origin}/git-proxy`;
}
