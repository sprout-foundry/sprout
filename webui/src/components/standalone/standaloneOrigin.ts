/**
 * standaloneOrigin — the standalone pages' postMessage trust model.
 *
 * The embed protocol is postMessage to window.parent. Outbound messages
 * carry file contents (save events), so they must go to the embedding
 * origin, not '*'. Inbound messages must come from the same origin —
 * otherwise any page on the internet could iframe an embed URL and
 * impersonate the host (drive open/save/confirm).
 *
 * Trust resolution, in order:
 *   1. ?parentOrigin=<origin> query param — the host declares itself.
 *      Accepted only when it matches document.referrer's origin (a
 *      forgerer can put any param on a URL, but the browser controls
 *      referrer for real framings). document.referrer may be empty
 *      (referrerpolicy=no-referrer) — then the declared origin is
 *      trusted as-is: framing the URL is already required to drive the
 *      protocol, and the frame-ancestors header is the real gate.
 *   2. document.referrer's origin — when no param is present.
 *   3. null — unknown host: inbound messages are accepted on the
 *      source tag alone (legacy native-shell behavior) and outbound
 *      messages use '*' (window.parent may be any origin).
 *
 * Native hosts (WKWebView shells) satisfy neither 1 nor 2 — they get
 * case 3 and keep working unchanged.
 */

let trustedOrigin: string | null | undefined;

/** Resolve (once) the origin the embedding parent is trusted at. */
export function resolveParentOrigin(): string | null {
  if (trustedOrigin !== undefined) return trustedOrigin;

  try {
    const declared = new URLSearchParams(window.location.search).get('parentOrigin')?.trim() ?? '';
    const referrerOrigin = originOf(document.referrer);

    if (declared) {
      // Only honor a declared origin the browser corroborates. With no
      // referrer to check against, take the declaration (see module doc).
      trustedOrigin = !referrerOrigin || declared === referrerOrigin ? declared : referrerOrigin;
    } else {
      trustedOrigin = referrerOrigin;
    }
  } catch {
    trustedOrigin = null;
  }

  return trustedOrigin;
}

/** The origin to post to: the trusted parent, or '*' when unknown. */
export function postTargetOrigin(): string {
  return resolveParentOrigin() ?? '*';
}

/** Whether a message event came from the trusted parent. */
export function isFromTrustedParent(ev: MessageEvent): boolean {
  const trusted = resolveParentOrigin();
  if (trusted) return ev.origin === trusted;
  // Unknown parent: accept the source-tagged protocol from anyone, as the
  // pages always have. The iframe/framing headers are the real boundary.
  return true;
}

function originOf(url: string): string | null {
  if (!url) return null;
  try {
    return new URL(url).origin;
  } catch {
    return null;
  }
}

/** Test hook: clear the memoized resolution. */
export function __resetParentOrigin(): void {
  trustedOrigin = undefined;
}
