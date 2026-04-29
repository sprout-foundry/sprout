// Placeholder — will be filled in
export interface GitFile {
  path: string;
  status: 'modified' | 'added' | 'deleted' | 'renamed' | 'untracked';
}

export interface FileSection {
  title: string;
  files: GitFile[];
}

export interface GitSidebarPanelProps {
  files: FileSection[];
  onFileClick?: (file: GitFile) => void;
}

export interface GitStatusData {
  branch: string;
  ahead: number;
  behind: number;
  modified: number;
  added: number;
  deleted: number;
  untracked: number;
}

export default {};
