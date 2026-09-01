/**
 * Track R build-time native-git flag (compile-time constant after Vite `define`).
 *
 * Ships in the DEFAULT build too (imported by the short-circuit call sites) as an
 * inert compile-time constant that evaluates to `false` — mirroring
 * `nativeChatFlag`.
 */
export const NATIVE_GIT_ENABLED: boolean = import.meta.env.VITE_SPROUT_NATIVE_GIT === '1';
