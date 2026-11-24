/**
 * Barrel re-export so `workspaceFs` has one import surface without a
 * circular dependency (index resolves backends; this file is the flat API).
 */
export { createMemoryFs } from './memoryFs';
export { createNativeBridgeFs, detectBridgeCall } from './nativeBridgeFs';
export { createRestFs } from './restFs';
export { cloneRepo, listRepos as listProjectFolderRepos, removeRepo, repoDir, parseRepoRef } from './projectFolderGit';
export type { CloneProgress, CloneOpts, CloneResult } from './projectFolderGit';
export { getProjectFolderFs } from './index';
export { isProjectFolderFs, normalizeFsPath, WRITE_BATCH_CAP } from './types';
export type {
  BatchResult,
  FsEntry,
  FsOk,
  FsResult,
  ListResult,
  ReadResult,
  StatResult,
  ProjectFolderFs,
  WriteEntry,
} from './types';
