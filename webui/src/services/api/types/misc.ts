/**
 * Miscellaneous API types (changelog, review, terminal, chat, file operations).
 */

// Re-export changelog types from @sprout/ui
export type { ChangelogResponse, ChangesResponse, RevisionDetailResponse, RollbackResponse } from '@sprout/ui';

// ── Review types ────────────────────────────────────────────────────

export interface DeepReviewResponse {
  message: string;
  status: string;
  feedback: string;
  detailed_guidance?: string;
  suggested_new_prompt?: string;
  review_output: string;
  provider?: string;
  model?: string;
  warnings?: string[];
}

export interface DeepReviewFixResponse {
  message: string;
  result: string;
}

export interface DeepReviewFixStartResponse {
  message: string;
  job_id: string;
  session_id: string;
}

export interface DeepReviewFixStatusResponse {
  message: string;
  job_id: string;
  session_id: string;
  status: 'running' | 'completed' | 'error';
  logs: string[];
  next_index: number;
  result: string;
  error: string;
}

// ── Terminal types ──────────────────────────────────────────────────

export interface TerminalHistoryResponse {
  history: string[];
  count: number;
}

export interface AddTerminalHistoryResponse {
  message: string;
  command: string;
}

// ── Chat types ──────────────────────────────────────────────────────

export interface UploadImageResponse {
  path: string;
  filename: string;
}

// ── File operations types ───────────────────────────────────────────

export interface CreateItemResponse {
  message: string;
  path: string;
}

export interface DeleteItemResponse {
  message: string;
  path: string;
}

export interface RenameItemResponse {
  message: string;
  old_path: string;
  new_path: string;
}
