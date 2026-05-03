/**
 * API response types for revision/changelog endpoints.
 * Import from @sprout/ui rather than defining locally in webui.
 */
import type { Revision, RevisionDetailFile } from './revision';

/** Response from GET /api/history/changelog */
export interface ChangelogResponse {
  message: string;
  revisions: Revision[];
}

/** Response from GET /api/history/changes */
export interface ChangesResponse {
  message: string;
  changes: Revision[];
}

/** Response from GET /api/history/revision?revision_id=... */
export interface RevisionDetailResponse {
  message: string;
  revision: {
    revision_id: string;
    timestamp: string;
    description: string;
    files: RevisionDetailFile[];
  };
}

/** Response from POST /api/history/rollback */
export interface RollbackResponse {
  message: string;
  revision_id: string;
}
