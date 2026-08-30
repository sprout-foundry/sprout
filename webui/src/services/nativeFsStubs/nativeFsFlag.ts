/** Track R build-time native-FS flag (compile-time constant after Vite `define`). */
export const NATIVE_FS_ENABLED: boolean = import.meta.env.VITE_SPROUT_NATIVE_FS === '1';
