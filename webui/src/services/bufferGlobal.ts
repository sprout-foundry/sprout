/**
 * Installs the global `Buffer` that isomorphic-git requires.
 *
 * isomorphic-git's smart-HTTP packet reader throws "Missing Buffer dependency"
 * without a global `Buffer`, so a clone fails right after the ref
 * advertisement. Every module that imports isomorphic-git imports this one
 * first, which makes the polyfill travel with the git code: the standalone app
 * and the `@sprout-foundry/workspace` package both get it without the host
 * page having to install one. An existing global is left alone.
 */

import { Buffer } from 'buffer';

/** Install `Buffer` on `scope` unless it already has one. */
export function installBufferGlobal(scope: { Buffer?: unknown } = globalThis as { Buffer?: unknown }): void {
  if (typeof scope.Buffer === 'undefined') {
    scope.Buffer = Buffer;
  }
}

installBufferGlobal();
