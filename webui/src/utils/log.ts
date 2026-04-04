/* eslint-disable no-console */
import { useMemo } from 'react';
import { useNotifications } from '../contexts/NotificationContext';

// Default notification duration in milliseconds
export const DEFAULT_NOTIFICATION_DURATION = 5000;

// Default title for log-based notifications
export const DEFAULT_LOG_TITLE = 'Application Log';

/**
 * Debug log - only shows in non-production environments
 */
export function debugLog(...args: unknown[]) {
  if (process.env.NODE_ENV !== 'production') {
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
  console.error(message);

  // showNotification is not supported in non-React context
  // Use useLog() hook for notification support
  if (options?.showNotification) {
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
  console.warn(message);

  if (options?.showNotification) {
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
  console.info(message);

  if (options?.showNotification) {
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
  console.log('[SUCCESS]', message);

  if (options?.showNotification) {
    console.warn('showNotification option is only available when using the useLog() hook inside React components');
  }
}

/**
 * React hook that provides log functions with automatic notification support
 * This should be used instead of the plain functions when inside React components
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
        console.error(message);
        addNotification('error', options?.title || DEFAULT_LOG_TITLE, message, options?.duration);
      },

      warn: (
        message: string,
        options?: {
          title?: string;
          duration?: number;
        },
      ) => {
        console.warn(message);
        addNotification('warning', options?.title || DEFAULT_LOG_TITLE, message, options?.duration);
      },

      info: (
        message: string,
        options?: {
          title?: string;
          duration?: number;
        },
      ) => {
        console.info(message);
        addNotification('info', options?.title || DEFAULT_LOG_TITLE, message, options?.duration);
      },

      success: (
        message: string,
        options?: {
          title?: string;
          duration?: number;
        },
      ) => {
        console.log('[SUCCESS]', message);
        addNotification('success', options?.title || DEFAULT_LOG_TITLE, message, options?.duration);
      },
    }),
    [addNotification],
  );

  return log;
}

/**
 * Type for log levels
 */
export type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'success';

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
