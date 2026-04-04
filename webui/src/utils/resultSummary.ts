/**
 * Utility functions for extracting summaries from various data types
 * and formatting tool results for display
 */

import { debugLog } from './log';
import { stripAnsiCodes } from './ansi';

/**
 * Truncate text to maxLength characters, adding "..." if needed
 * @param value - The text to truncate
 * @param maxLength - Maximum length before truncation
 * @returns Truncated text or original if within limit
 */
export function truncateText(value: string, maxLength: number): string {
  if (value.length <= maxLength) {
    return value;
  }
  return `${value.slice(0, Math.max(0, maxLength - 1)).trimEnd()}...`;
}

/**
 * Format tool detail content - attempts to pretty-print JSON, returns as-is on failure
 * @param content - The content to format
 * @returns Formatted JSON or original content
 */
export function formatToolDetail(content: string): string {
  try {
    const parsed = JSON.parse(content);
    return JSON.stringify(parsed, null, 2);
  } catch (err) {
    debugLog('[resultSummary] formatToolDetail JSON parse failed:', err);
    return content;
  }
}

/**
 * Recursively extract a text summary from various value types
 * @param value - The value to extract summary from
 * @returns Text summary or null if no summary found
 */
export function extractResultSummary(value: unknown): string | null {
  if (typeof value === 'string') {
    const cleaned = stripAnsiCodes(value).replace(/\s+/g, ' ').trim();
    return cleaned || null;
  }

  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }

  if (Array.isArray(value)) {
    for (const item of value) {
      const summary = extractResultSummary(item);
      if (summary) {
        return summary;
      }
    }
    return null;
  }

  if (value && typeof value === 'object') {
    const record = value as Record<string, unknown>;
    const priorityKeys = ['summary', 'result', 'response', 'output', 'final_answer', 'message', 'content'];
    for (const key of priorityKeys) {
      if (key in record) {
        const summary = extractResultSummary(record[key]);
        if (summary) {
          return summary;
        }
      }
    }

    for (const nestedValue of Object.values(record)) {
      const summary = extractResultSummary(nestedValue);
      if (summary) {
        return summary;
      }
    }
  }

  return null;
}

/**
 * Extract a preview of subagent result from raw result string
 * @param rawResult - Raw result string from subagent execution
 * @returns Truncated preview or undefined if no valid result
 */
export function getSubagentResultPreview(rawResult?: string): string | undefined {
  if (!rawResult) {
    return undefined;
  }

  const cleaned = stripAnsiCodes(rawResult).trim();
  if (!cleaned) {
    return undefined;
  }

  try {
    const parsed = JSON.parse(cleaned);
    const summary = extractResultSummary(parsed);
    return summary
      ? truncateText(summary, 220)
      : truncateText(formatToolDetail(cleaned).replace(/\s+/g, ' ').trim(), 220);
  } catch (err) {
    debugLog('[resultSummary] getSubagentResultPreview JSON parse failed:', err);
    return truncateText(cleaned.replace(/\s+/g, ' ').trim(), 220);
  }
}
