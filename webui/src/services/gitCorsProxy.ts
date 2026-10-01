import { isCloud } from '../config/mode';
import { NATIVE_FS_ENABLED } from './nativeFsStubs/nativeFsFlag';
import { NATIVE_GIT_ENABLED } from './nativeGitStubs/nativeGitFlag';

/**
 * isomorphic-git `corsProxy` for the hosted browser IDE. GitHub's git
 * endpoints send no CORS headers, so a page can't clone, pull or push against
 * them directly; the platform serves the IDE and forwards git smart-HTTP at
 * /git-proxy (same origin, so the session cookie authenticates it and the
 * platform can add the user's connected GitHub token). Native shells run git
 * themselves or don't enforce CORS, so they get no proxy.
 */
export function gitCorsProxy(): string | undefined {
  if (!isCloud || NATIVE_FS_ENABLED || NATIVE_GIT_ENABLED || typeof window === 'undefined') return undefined;
  return `${window.location.origin}/git-proxy`;
}
