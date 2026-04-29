/** Which section a git file belongs to */
export type FileSection = 'staged' | 'modified' | 'untracked' | 'deleted';

/** Standard file sections configuration */
export const FILE_SECTIONS: Array<{ id: FileSection; title: string }> = [
  { id: 'staged', title: 'Staged' },
  { id: 'modified', title: 'Modified' },
  { id: 'untracked', title: 'Untracked' },
  { id: 'deleted', title: 'Deleted' },
];

/** Build a unique selection key for a file in a section */
export const selectionKey = (section: FileSection, path: string): string => `${section}:${path}`;

/** Parse a selection key back into section + path */
export const parseSelectionKey = (key: string): { section: FileSection; path: string } | null => {
  const separatorIndex = key.indexOf(':');
  if (separatorIndex <= 0) return null;
  const section = key.slice(0, separatorIndex) as FileSection;
  const path = key.slice(separatorIndex + 1);
  if (!path || !FILE_SECTIONS.some((s) => s.id === section)) return null;
  return { section, path };
};

/** File entry in git status */
export interface GitFile {
  path: string;
  status: string;
  changes?: {
    additions: number;
    deletions: number;
  };
}

/** Commit summary used in history listings */
export interface GitCommitSummary {
  hash: string;
  short_hash: string;
  author: string;
  date: string;
  message: string;
  ref_names?: string;
}

/** A file changed in a commit */
export interface GitCommitFileEntry {
  path: string;
  status: string;
}

/** Full commit detail including diff */
export interface GitCommitDetail extends GitCommitSummary {
  subject: string;
  files: GitCommitFileEntry[];
  diff: string;
  stats: string;
}

/** Full git status response */
export interface GitStatusData {
  branch: string;
  ahead: number;
  behind: number;
  staged: GitFile[];
  modified: GitFile[];
  untracked: GitFile[];
  deleted: GitFile[];
  renamed: GitFile[];
  clean: boolean;
  truncated: boolean;
}
