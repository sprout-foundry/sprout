import type { LogEntry } from '../types/app';
import { MAX_LOG_ENTRIES } from '../constants/app';

/**
 * Append a log entry to the array, capping the total at MAX_LOG_ENTRIES.
 * Keeps the most recent entries and discards the oldest.
 */
export function appendCappedLog(logs: LogEntry[], entry: LogEntry): LogEntry[] {
  if (logs.length >= MAX_LOG_ENTRIES) {
    return [...logs.slice(-(MAX_LOG_ENTRIES - 1)), entry];
  }
  return [...logs, entry];
}
