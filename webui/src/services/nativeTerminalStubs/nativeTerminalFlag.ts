/**
 * Track R build-time native-terminal flag (compile-time constant after Vite `define`).
 *
 * Ships in the DEFAULT build too (imported by the short-circuit call sites) as an
 * inert compile-time constant that evaluates to `false` — mirroring `nativeFsFlag`.
 */
export const NATIVE_TERMINAL_ENABLED: boolean = import.meta.env.VITE_SPROUT_NATIVE_TERMINAL === '1';
