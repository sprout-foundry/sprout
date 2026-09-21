/**
 * Design status client — SP-140-6 §6b/§6c.
 *
 * Types for GET /api/design/status (the read-only aggregate over the same
 * pkg/design scanners the agent tools read) and the fetch helper the health
 * strip uses. Shape-mirrors pkg/design/status.go — keep the two in step.
 */

import { useCallback } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';

/** One validator finding in the status payload (the §1g shape). */
export interface DesignStatusFinding {
  file: string;
  line: number;
  severity: string;
  message: string;
  rule: string;
}

/**
 * The severity vocabulary the Go validator emits (pkg/design/finding.go:
 * error/warn/info/fix). Both the chips' classes and their click-through
 * filters must use these spellings — a 'warning' spelling would render but
 * never click through.
 */
export const FINDING_SEVERITIES = {
  error: 'error',
  warnings: ['warn', 'fix'],
  info: 'info',
} as const;

/** Validation tallies (authoritative) plus the capped finding list. */
export interface DesignStatusValidation {
  errors: number;
  warnings: number;
  infos: number;
  findings: DesignStatusFinding[];
}

/** One §5c drift direction row. */
export interface DesignStatusDriftRow {
  ahead: boolean;
  synced: boolean;
  count: number;
  summary?: string;
  remedy?: string;
  nextStep?: string;
}

/** One pending feedback target. */
export interface DesignStatusFeedbackEntry {
  target: string;
  unresolved: number;
  status?: string;
}

/** The GET /api/design/status payload (pkg/design/status.go DesignStatus). */
export interface DesignStatus {
  exists: boolean;
  validation: DesignStatusValidation;
  drift: {
    designAhead: DesignStatusDriftRow;
    codeAhead: DesignStatusDriftRow;
    synced: boolean;
  };
  feedback: {
    pendingCount: number;
    pending: DesignStatusFeedbackEntry[];
  };
  /** Dotted token path -> lexical alias reference count (§7c input). */
  tokenRefs?: Record<string, number>;
  summary: string;
}

/**
 * Fetch the design status. Returns null on any failure — the health strip
 * renders its idle state rather than an error (the endpoint itself never
 * errors on a missing tree; null here means transport trouble).
 */
export async function fetchDesignStatus(fetchFn: typeof fetch): Promise<DesignStatus | null> {
  try {
    const response = await fetchFn('/api/design/status');
    if (!response.ok) return null;
    return (await response.json()) as DesignStatus;
  } catch {
    return null;
  }
}

/**
 * React binding for the health strip and its consumers. Refetches through the
 * returned callback; callers own their polling cadence (the strip follows the
 * live tree's rhythm — focus/interval — rather than its own).
 */
export function useDesignStatusFetcher() {
  const fetchFn = useSproutFetch();
  return useCallback(() => fetchDesignStatus(fetchFn), [fetchFn]);
}
