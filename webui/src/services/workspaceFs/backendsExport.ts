/**
 * Barrel re-export so `workspaceFs` has one import surface without a
 * circular dependency (index resolves backends; this file is the flat API).
 */
export { createMemoryFs } from './memoryFs';
export { createNativeBridgeFs, detectBridgeCall } from './nativeBridgeFs';
export { createRestFs } from './restFs';
export { cloneRepo, listRepos as listWorkspaceRepos, removeRepo, repoDir, parseRepoRef } from './workspaceGit';
export type { CloneProgress, CloneOpts, CloneResult } from './workspaceGit';
export { getWorkspaceFs } from './index';
export {
  isWorkspaceFs,
  normalizeFsPath,
  WRITE_BATCH_CAP,
} from './types';
export type {
  BatchResult,
  FsEntry,
  FsOk,
  FsResult,
  ListResult,
  ReadResult,
  StatResult,
  WorkspaceFs,
  WriteEntry,
} from './types';
