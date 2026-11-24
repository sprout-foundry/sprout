/**
 * Ship mode payload.
 *
 * Ship is the deploy lens on the shared project conversation: it shows what is
 * live, what was deployed, and how to ship or roll back. The data comes from
 * the app, not from the shell — the shell renders these values and nothing
 * else, so it stays a pure component that tests can drive with plain objects.
 *
 * The shapes mirror the backend deploy model (Go `Deployment`/`StatusState`):
 * a deployment has an id, a kind (preview vs production), the version it
 * built, the URL it is served at, when it was recorded, and its lifecycle
 * state. The app maps that model onto these fields at the seam; nothing here
 * reaches for a network or a store, so a test can render the surface without a
 * server.
 *
 * History entries carry a plan revision and an optional change summary. The
 * summary is a slot: it is whatever the payload supplies today (a plan
 * revision, or a one-line description), and a richer summarizer fills the same
 * field later without a shape change.
 */

/** Lifecycle state of a deployment, mirroring the backend's status values. */
export type ShipDeployState = 'queued' | 'deploying' | 'ready' | 'failed' | 'rolled_back';

/** Preview is per-deployment and automatic; production needs confirmation. */
export type ShipDeployKind = 'preview' | 'production';

/** One recorded deploy in the project's history. */
export interface ShipHistoryEntry {
  /** Target-assigned deployment id; stable for the life of the deployment. */
  id: string;
  /** Whether this was a preview or a production deploy. */
  kind: ShipDeployKind;
  /** The version that was built (starter/plan version). */
  version: string;
  /** The plan revision this deploy came from, when the payload knows it. */
  planRevision?: string;
  /** Address the deployment is served at (per-deployment URL for a preview). */
  url?: string;
  /** When the deployment was recorded, ISO-8601 UTC. */
  createdAt: string;
  /** Lifecycle state at the time the payload was assembled. */
  state: ShipDeployState;
  /**
   * Short human summary of what changed in this deploy — the change summary
   * slot. Whatever the payload carries (a template summary today, a
   * model-written one later) renders here; absent means the row shows the
   * version and plan revision alone.
   */
  summary?: string;
}

/** What is live right now: the address, the version, and when it last shipped. */
export interface ShipLiveStatus {
  /** The live site's URL, when a production deployment is serving. */
  url?: string;
  /** The version currently live (the latest ready production deploy). */
  version?: string;
  /** When the live version was deployed, ISO-8601 UTC. */
  deployedAt?: string;
  /** The lifecycle state of the current deploy, when one is in flight or known. */
  state?: ShipDeployState;
}

/**
 * Whether the deploy action is offered, and why not when it is withheld.
 *
 * Kept as plain data so a test (and the surface) can render the blocked reason
 * without interpreting app state: the app decides, the surface shows.
 */
export interface ShipDeployAvailability {
  /** True when a deploy may be started from here. */
  canDeploy: boolean;
  /** A short reason shown beside a blocked deploy action. */
  blockedReason?: string;
}

export interface ShipPayload {
  /** What is live now: URL, version, last deploy. */
  live: ShipLiveStatus;
  /** Whether the deploy action is offered. */
  availability: ShipDeployAvailability;
  /**
   * Deploy history, newest first — the payload supplies the order; the surface
   * renders it as given. It does not sort, so the app seam owns ordering
   * (correct order is a supply-side obligation, not a rendering trick).
   */
  history: ShipHistoryEntry[];
  /** True while the app is starting a deploy (the action shows progress). */
  isDeploying?: boolean;
  /** True while the app is rolling back (the action shows progress). */
  isRollingBack?: boolean;
  /** The last deploy/rollback error, if any. */
  error?: string | null;
  /** Start a deploy. The app owns preview/production and confirmation. */
  onDeploy?: () => void;
  /** Roll back to the deployment before `id`. */
  onRollback?: (id: string) => void;
  /** Open a history entry's detail (its URL or the changes it carried). */
  onOpenEntry?: (id: string) => void;
}
