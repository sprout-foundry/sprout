/**
 * Shared constants for terminal components.
 * FONT_SIZE_DEFAULT is re-exported from @sprout/ui to avoid duplication.
 * COPY_ON_SELECT_* constants are webui-specific (localStorage-based toggle).
 */

export { FONT_SIZE_DEFAULT } from '@sprout/ui';

export const COPY_ON_SELECT_DEFAULT = false;

export const COPY_ON_SELECT_STORAGE_KEY = 'sprout-terminal-copy-on-select';
