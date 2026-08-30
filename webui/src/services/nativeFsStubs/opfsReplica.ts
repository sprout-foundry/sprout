/**
 * Track R (--native-fs) stub for services/opfsReplica.ts.
 *
 * Vite's conditional alias (webui/vite.config.ts) swaps every import of
 * services/opfsReplica for this module when the dist build is invoked with
 * VITE_SPROUT_NATIVE_FS=1 (scripts/build-webui-dist.mjs --native-fs). The
 * OPFS replica (Origin Private File System) is a local FS-backed sync layer
 * that the native shell replaces, so it is excluded from the bundle.
 *
 * No runtime dependency on the real module: the interfaces are re-exported
 * via `export type` (erased at compile time). The class and the singleton
 * are safe no-op stubs that degrade gracefully if ever touched at runtime.
 */

import type {
  OPFSFileMetadata,
  OPFSReplicaStatus,
  OPFSManifestEntry,
  OPFSPatchOp,
} from '../opfsReplica';

// Re-export the type surface (type-only, erased at compile time).
export type {
  OPFSFileMetadata,
  OPFSReplicaStatus,
  OPFSManifestEntry,
  OPFSPatchOp,
} from '../opfsReplica';

/**
 * No-op stand-in for the real `OPFSReplicaService`. Reports "unavailable"
 * and safe empty values so callers degrade gracefully in the native-fs build.
 */
export class OPFSReplicaService {
  /** OPFS is not used in the native-fs build — report unavailable. */
  static isAvailable(): boolean {
    return false;
  }

  async init(): Promise<void> {
    // no-op
  }

  async initReplica(
    _manifest: OPFSManifestEntry[],
  ): Promise<{ fileCount: number; totalSize: number }> {
    return { fileCount: 0, totalSize: 0 };
  }

  async applyPatch(_patch: OPFSPatchOp): Promise<void> {
    // no-op
  }

  async getFile(
    _path: string,
  ): Promise<{ exists: boolean; content: string | null; metadata: OPFSFileMetadata | null }> {
    return { exists: false, content: null, metadata: null };
  }

  getStatus(): OPFSReplicaStatus {
    return { fileCount: 0, totalSize: 0, lastSyncTimestamp: null };
  }

  async storeMetadata(_path: string, _metadata: Partial<OPFSFileMetadata>): Promise<void> {
    // no-op
  }
}

/** Singleton stand-in mirroring the real module's exported instance. */
export const opfsReplicaService = new OPFSReplicaService();