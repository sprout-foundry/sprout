/**
 * standaloneOrigin tests — the postMessage trust resolution.
 *
 * jsdom's document.referrer is manipulable via replaceState-adjacent
 * history tricks only through jsdom internals; these tests drive the
 * module through its actual inputs by rewriting document.referrer
 * directly (configurable in jsdom) and the query string.
 */
// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { __resetParentOrigin, isFromTrustedParent, postTargetOrigin, resolveParentOrigin } from './standaloneOrigin';

function setReferrer(value: string) {
  Object.defineProperty(document, 'referrer', { configurable: true, value });
}

describe('standaloneOrigin', () => {
  beforeEach(() => {
    __resetParentOrigin();
    setReferrer('');
    window.history.replaceState(null, '', '/editor.html');
  });

  afterEach(() => {
    setReferrer('');
    __resetParentOrigin();
  });

  it('returns null (unknown) with no param and no referrer → post target is *', () => {
    expect(resolveParentOrigin()).toBeNull();
    expect(postTargetOrigin()).toBe('*');
  });

  it('adopts the referrer origin when no param is declared', () => {
    setReferrer('https://host.example/dashboard');
    expect(resolveParentOrigin()).toBe('https://host.example');
    expect(postTargetOrigin()).toBe('https://host.example');
  });

  it('accepts a declared parentOrigin that matches the referrer', () => {
    setReferrer('https://host.example/page');
    window.history.replaceState(null, '', '/editor.html?parentOrigin=https%3A%2F%2Fhost.example');
    expect(resolveParentOrigin()).toBe('https://host.example');
  });

  it('rejects a declared parentOrigin that contradicts the referrer (forgerer param)', () => {
    setReferrer('https://real-host.example/frame');
    window.history.replaceState(null, '', '/editor.html?parentOrigin=https%3A%2F%2Fevil.example');
    expect(resolveParentOrigin()).toBe('https://real-host.example');
  });

  it('trusts a declared parentOrigin when the referrer is withheld (no-referrer policy)', () => {
    setReferrer('');
    window.history.replaceState(null, '', '/editor.html?parentOrigin=https%3A%2F%2Fhost.example');
    expect(resolveParentOrigin()).toBe('https://host.example');
  });

  describe('isFromTrustedParent', () => {
    it('accepts only the trusted origin when one is known', () => {
      setReferrer('https://host.example/app');
      expect(resolveParentOrigin()).toBe('https://host.example');

      const mk = (origin: string) => ({ origin }) as MessageEvent;
      expect(isFromTrustedParent(mk('https://host.example'))).toBe(true);
      expect(isFromTrustedParent(mk('https://evil.example'))).toBe(false);
      expect(isFromTrustedParent(mk('null'))).toBe(false);
    });

    it('accepts source-tagged messages from anyone when the parent is unknown (native shells)', () => {
      expect(resolveParentOrigin()).toBeNull();
      const mk = (origin: string) => ({ origin }) as MessageEvent;
      expect(isFromTrustedParent(mk('https://whatever.example'))).toBe(true);
      expect(isFromTrustedParent(mk('null'))).toBe(true);
    });
  });
});
