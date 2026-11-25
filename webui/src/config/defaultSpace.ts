/**
 * Configured default project folder mode.
 *
 * An embedding shell — or the build itself — can point new sessions at a mode
 * other than the built-in default by setting `VITE_DEFAULT_WORKSPACE_MODE`.
 * Like the other config surfaces (`config/layout.ts`, `config/mode.ts`) the
 * value is read at module load, so it is a build-time choice.
 *
 * An embedding shell that installs before the shell renders can also set the
 * default at runtime with `overrideDefaultSpace`; tests use the same
 * seam to simulate a configured default without a rebuild.
 *
 * This module only reports the *configured* id (or `null` when nothing is
 * set). Whether that id actually names a mode the project folder offers is the
 * registry's concern: an id that points at an unavailable or unregistered
 * mode falls back to the built-in default (`spaces/registry.ts`).
 */

const BUILD_DEFAULT: string | null =
  typeof import.meta.env.VITE_DEFAULT_WORKSPACE_MODE === 'string' && import.meta.env.VITE_DEFAULT_WORKSPACE_MODE !== ''
    ? import.meta.env.VITE_DEFAULT_WORKSPACE_MODE
    : null;

let runtimeDefault: string | null = BUILD_DEFAULT;

/**
 * Override the default project folder mode at runtime.
 *
 * Pass a mode id to point new sessions there, or `null` to fall back to the
 * build-time value (and, when that is unset, the built-in default). An
 * embedding shell calls this before the shell renders.
 */
export function overrideDefaultSpace(id: string | null): void {
  runtimeDefault = id;
}

/** The configured default project folder mode, or `null` when nothing is set. */
export function configuredDefaultSpace(): string | null {
  return runtimeDefault;
}
