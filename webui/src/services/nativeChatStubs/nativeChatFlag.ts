/**
 * Track R build-time native-chat flag (compile-time constant after Vite `define`).
 *
 * Ships in the DEFAULT build too (imported by the short-circuit call sites) as an
 * inert compile-time constant that evaluates to `false` — mirroring
 * `nativeTerminalFlag`.
 */
export const NATIVE_CHAT_ENABLED: boolean = import.meta.env.VITE_SPROUT_NATIVE_CHAT === '1';
