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
export const MAX_PERSISTED_LOGS = 1000;
export const MAX_LOG_ENTRIES = 1000;
