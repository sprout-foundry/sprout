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
 * re-export — the surface below is the entire module.
 *
 * R-2w (manifest-driven FS deferral): when the runtime gate passes —
 * the compile-time `NATIVE_FS_ENABLED` flag is on, the shell bridge is
 * present, `capabilities.fs === true`, and the served manifest carries a
 * ratified `fs` exclusion — reads and writes are routed through the
 * bridge's files channel (`readWorkspaceFile` / `writeWorkspaceFile`)
 * and returned as synthesized `Response`s (see the leaf module in
 * services/nativeFs/). When the gate does NOT pass (no bridge, no
 * manifest, seam-only, shell doesn't declare fs, or getCapabilities
 * fails), the functions throw exactly as before (see the ADR-0008
 * deferral-wiring section and docs/WEBUI_DECOUPLING_AUDIT.md §4).
 */

import { nativeFsGate, nativeReadWorkspaceFile, nativeWriteWorkspaceFile, normalizeWorkspacePath } from '../nativeFs';

function notProvidedNative(op: string): Error {
  return new Error(
    `fileAccess.${op}: provided natively by the shell (Track R --native-fs); ` +
      `the webui FS module was excluded from this build.`,
  );
}

export async function readFileWithConsent(filePath: string): Promise<Response> {
  const gate = await nativeFsGate();
  if (!gate.active) {
    // Gate-fail: no bridge / no manifest / seam-only / fs not declared or
    // ratified → throw exactly as the pre-R-2w stub did.
    throw notProvidedNative('readFileWithConsent');
  }
  // Normalize webui path → workspace-relative (rejects `..` / empty /
  // absolute paths client-side, before ever hitting the bridge).
  const wsPath = normalizeWorkspacePath(filePath);
  return nativeReadWorkspaceFile(wsPath);
}

export async function writeFileWithConsent(filePath: string, content: string): Promise<Response> {
  const gate = await nativeFsGate();
  if (!gate.active) {
    throw notProvidedNative('writeFileWithConsent');
  }
  const wsPath = normalizeWorkspacePath(filePath);
  return nativeWriteWorkspaceFile(wsPath, content);
}
