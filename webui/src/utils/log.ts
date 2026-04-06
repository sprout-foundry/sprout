/* eslint-disable no-console */
import { useMemo } from 'react';
import { useNotifications } from '../contexts/NotificationContext';

// Default notification duration in milliseconds
export const DEFAULT_NOTIFICATION_DURATION = 5000;

// Default title for log-based notifications
export const DEFAULT_LOG_TITLE = 'Application Log';

// Log level order: debug=0, info=1, success=2, warn=3, error=4
export const Levels = {
  debug: 0,
  info: 1,
  success: 2,
  warn: 3,
  error: 4,
} as const;

// Type for log levels (key of Levels)
export type LogLevel = keyof typeof Levels;

// Module-level minimum log level
// Default to debug in non-production, warn in production
const getProductionLevel = () => {
  return process.env.NODE_ENV === 'production' ? Levels.warn : Levels.debug;
};
let minLevel: number = getProductionLevel();

/**
 * Set the minimum log level for all log functions.
 * @param level - Numeric level (0=debug, 1=info, 2=success, 3=warn, 4=error)
 */
export function setMinLevel(level: number): void {
  minLevel = level;
}

/**
 * Get the current minimum log level.
 * @returns Numeric level (0=debug, 1=info, 2=success, 3=warn, 4=error)
 */
export function getMinLevel(): number {
  return minLevel;
}

/**
 * Debug log - only shows in non-production environments AND if minLevel <= debug
 */
export function debugLog(...args: unknown[]) {
  // Check NODE_ENV first (existing behavior)
  if (process.env.NODE_ENV !== 'production' && minLevel <= Levels.debug) {
    console.log(...args);
  }
}

/**
 * Console error logger with optional notification
 * Note: showNotification only works when called from within React components using useLog() hook
 */
export function error(
  message: string,
  options?: {
    showNotification?: boolean;
    title?: string;
    duration?: number;
  },
) {
  // Check minLevel before writing to console
  if (minLevel <= Levels.error) {
    console.error(message);
  }

  // showNotification is not supported in non-React context
  // Use useLog() hook for notification support
  if (options?.showNotification && minLevel <= Levels.warn) {
    console.warn('showNotification option is only available when using the useLog() hook inside React components');
  }
}

/**
 * Console warning logger with optional notification
 * Note: showNotification only works when called from within React components using useLog() hook
 */
export function warn(
  message: string,
  options?: {
    showNotification?: boolean;
    title?: string;
    duration?: number;
  },
) {
  // Check minLevel before writing to console
  if (minLevel <= Levels.warn) {
    console.warn(message);
  }

  if (options?.showNotification && minLevel <= Levels.warn) {
    console.warn('showNotification option is only available when using the useLog() hook inside React components');
  }
}

/**
 * Console info logger with optional notification
 * Note: showNotification only works when called from within React components using useLog() hook
 */
export function info(
  message: string,
  options?: {
    showNotification?: boolean;
    title?: string;
    duration?: number;
  },
) {
  // Check minLevel before writing to console
  if (minLevel <= Levels.info) {
    console.info(message);
  }

  if (options?.showNotification && minLevel <= Levels.warn) {
    console.warn('showNotification option is only available when using the useLog() hook inside React components');
  }
}

/**
 * Console success logger with optional notification
 * Note: showNotification only works when called from within React components using useLog() hook
 */
export function success(
  message: string,
  options?: {
    showNotification?: boolean;
    title?: string;
    duration?: number;
  },
) {
  // Check minLevel before writing to console
  if (minLevel <= Levels.success) {
    console.log('[SUCCESS]', message);
  }

  if (options?.showNotification && minLevel <= Levels.warn) {
    console.warn('showNotification option is only available when using the useLog() hook inside React components');
  }
}

/**
 * React hook that provides log functions with automatic notification support
 * This should be used instead of the plain functions when inside React components
 *
 * NOTE: `minLevel` is a module-level variable, not React state — every invocation
 * of the methods below reads the live value. It does NOT need to appear in the
 * useMemo dependency array.
 */
export function useLog() {
  const { addNotification } = useNotifications();

  const log = useMemo(
    () => ({
      debug: (...args: unknown[]) => debugLog(...args),

      error: (
        message: string,
        options?: {
          title?: string;
          duration?: number;
        },
      ) => {
        // Check minLevel for console output
        if (minLevel <= Levels.error) {
          console.error(message);
        }
        // Notifications always fire regardless of minLevel
        addNotification('error', options?.title || DEFAULT_LOG_TITLE, message, options?.duration);
      },

      warn: (
        message: string,
        options?: {
          title?: string;
          duration?: number;
        },
      ) => {
        // Check minLevel for console output
        if (minLevel <= Levels.warn) {
          console.warn(message);
        }
        // Notifications always fire regardless of minLevel
        addNotification('warning', options?.title || DEFAULT_LOG_TITLE, message, options?.duration);
      },

      info: (
        message: string,
        options?: {
          title?: string;
          duration?: number;
        },
      ) => {
        // Check minLevel for console output
        if (minLevel <= Levels.info) {
          console.info(message);
        }
        // Notifications always fire regardless of minLevel
        addNotification('info', options?.title || DEFAULT_LOG_TITLE, message, options?.duration);
      },

      success: (
        message: string,
        options?: {
          title?: string;
          duration?: number;
        },
      ) => {
        // Check minLevel for console output
        if (minLevel <= Levels.success) {
          console.log('[SUCCESS]', message);
        }
        // Notifications always fire regardless of minLevel
        addNotification('success', options?.title || DEFAULT_LOG_TITLE, message, options?.duration);
      },
    }),
    [addNotification],
  );

  return log;
}

/**
 * Interface for the log object returned by useLog
 */
export interface LogInterface {
  debug: (...args: unknown[]) => void;
  error: (message: string, options?: { title?: string; duration?: number }) => void;
  warn: (message: string, options?: { title?: string; duration?: number }) => void;
  info: (message: string, options?: { title?: string; duration?: number }) => void;
  success: (message: string, options?: { title?: string; duration?: number }) => void;
}
