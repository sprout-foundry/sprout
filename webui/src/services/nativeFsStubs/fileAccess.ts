/**
 * Track R (--native-fs) stub for services/fileAccess.ts.
 *
 * Vite's conditional alias (webui/vite.config.ts) swaps every import of
 * services/fileAccess for this module when the dist build is invoked with
 * VITE_SPROUT_NATIVE_FS=1 (scripts/build-webui-dist.mjs --native-fs). The
 * native shell provides external file read/write natively, so the real
 * module (consent handshake with the daemon) is hard-excluded from the
 * bundle.
 *
 * This file has NO runtime dependency on the real module. The real
 * fileAccess.ts exports no types of its own, so there is nothing to
 * re-export — the stub below is the entire surface.
 */

function notProvidedNative(op: string): Error {
  return new Error(
    `fileAccess.${op}: provided natively by the shell (Track R --native-fs); ` +
      `the webui FS module was excluded from this build.`,
  );
}

export function readFileWithConsent(_filePath: string): Promise<Response> {
  return Promise.reject(notProvidedNative('readFileWithConsent'));
}

export function writeFileWithConsent(_filePath: string, _content: string): Promise<Response> {
  return Promise.reject(notProvidedNative('writeFileWithConsent'));
}
