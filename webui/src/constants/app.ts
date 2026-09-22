/**
 * Application-level constants extracted from App.tsx
 */

export const APP_STATE_STORAGE_KEY = 'sprout:webui:state:v2';
export const INSTANCE_PID_STORAGE_KEY = 'sprout:webui:instancePid';
export const INSTANCE_SWITCH_RESET_KEY = 'sprout:webui:instanceSwitchReset';
/**
 * Active workspace mode, scoped per instance (`<key>:<pid>:<scope>`), so each
 * connected filesystem root remembers whether the user was in Code or Design.
 * See `webui/src/workspaces/`.
 */
export const WORKSPACE_MODE_STORAGE_KEY = 'sprout:webui:workspaceMode:v1';
/**
 * Per-mode chat session pin (SP-140-10c): which chat session each workspace
 * mode restores — `JSON { code?: string; design?: string }`, scoped per
 * instance PID + UI context exactly like `WORKSPACE_MODE_STORAGE_KEY`.
 * Design mode never falls back to a Code session (no pin = fresh chat).
 */
export const CHAT_MODE_PIN_STORAGE_KEY = 'sprout:webui:chatModePin:v1';
export const MAX_PERSISTED_LOGS = 1000;
export const MAX_LOG_ENTRIES = 1000;
