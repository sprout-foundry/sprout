/**
 * Git domain API types.
 */

export interface GitStatusEntry {
  path: string;
  status: string;
  staged: boolean;
}

export interface GitStatusResponse {
  message: string;
  in_git_repo: boolean;
  status: {
    branch: string;
    ahead: number;
    behind: number;
    staged: GitStatusEntry[];
    modified: GitStatusEntry[];
    untracked: GitStatusEntry[];
    deleted: GitStatusEntry[];
    renamed: GitStatusEntry[];
    truncated?: boolean;
    in_git_repo: boolean;
  };
  files: Array<{ path: string; status: string; staged?: boolean }>;
}

export interface GitBranchesResponse {
  message: string;
  current: string;
  branches: string[];
}

export interface GitBranchResponse {
  message: string;
  branch: string;
}

export interface GitPushPullResponse {
  message: string;
  output?: string;
}

export interface GitStageResponse {
  message: string;
  path: string;
}

export interface GitStageAllResponse {
  message: string;
}

export interface GitCommitResponse {
  message: string;
  commit: string;
}

export interface GitCommitMessageResponse {
  message: string;
  commit_message: string;
  provider?: string;
  model?: string;
  warnings?: string[];
}

export interface GitLogEntry {
  hash: string;
  short_hash: string;
  author: string;
  date: string;
  message: string;
  ref_names?: string;
}

export interface GitLogResponse {
  message: string;
  commits: GitLogEntry[];
  offset: number;
  limit: number;
  total: number;
}

export interface GitCommitDetailResponse {
  message: string;
  hash: string;
  short_hash: string;
  author: string;
  date: string;
  ref_names?: string;
  subject: string;
  files: Array<{ path: string; status: string }>;
  diff: string;
  stats: string;
}

export interface GitCommitFileDiffResponse {
  message: string;
  hash: string;
  path: string;
  diff: string;
}

export interface GitDiffResponse {
  message: string;
  path: string;
  has_staged: boolean;
  has_unstaged: boolean;
  staged_diff: string;
  unstaged_diff: string;
  diff: string;
}
