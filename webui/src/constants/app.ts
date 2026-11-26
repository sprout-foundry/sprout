/**
 * Application-level constants extracted from App.tsx
 */

export const APP_STATE_STORAGE_KEY = 'sprout:webui:state:v2';
export const INSTANCE_PID_STORAGE_KEY = 'sprout:webui:instancePid';
export const INSTANCE_SWITCH_RESET_KEY = 'sprout:webui:instanceSwitchReset';
/**
 * Active project folder mode, scoped per instance (`<key>:<pid>:<scope>`), so each
 * connected filesystem root remembers which mode the user was in.
 * See `webui/src/spaces/`.
 */
export const SPACE_STORAGE_KEY = 'sprout:webui:space:v1';
/** SPACE_STORAGE_KEY as earlier versions spelled it; read once, then moved. */
export const EARLIER_SPACE_STORAGE_KEY = 'sprout:webui:workspaceMode:v1';
export const MAX_PERSISTED_LOGS = 1000;
export const MAX_LOG_ENTRIES = 1000;
