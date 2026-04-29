export interface FileInfo {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: number;
  ext?: string;
  gitStatus?: 'modified' | 'untracked' | 'ignored';
  children?: FileInfo[];
}
