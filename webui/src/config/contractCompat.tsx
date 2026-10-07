/**
 * contractCompat.tsx — API contract version negotiation (SP-160 §160c).
 *
 * The daemon reports the API contract version (the OpenAPI `info.version` in
 * docs/api) on GET /api/bootstrap. This module holds the value THIS build was
 * built against (CONTRACT_VERSION) and the negotiation rules:
 *
 *   - a different MAJOR version is incompatible → the app refuses to start
 *     and shows a blocking screen (ContractRefusal) with a clear message;
 *   - a newer MINOR version on the daemon is forward-compatible → the app
 *     starts and logs a warning (the daemon may serve shapes this build does
 *     not know);
 *   - an equal or older minor, or a daemon that predates the field (no
 *     contractVersion), starts normally.
 *
 * CONTRACT_VERSION is the Web UI's half of a three-way pin: the Go side's
 * source of truth (pkg/webui.ContractVersion) and the hand-written seed
 * (docs/api/openapi.base.yaml info.version) are pinned to the same value by
 * Go tests, and TestContractVersionPinnedToWebUI pins this constant to the
 * Go one. Bump all of them together when the contract shape breaks.
 */

/**
 * The API contract version this UI build was compiled against.
 * Kept in lockstep with pkg/webui/contract.go (the Go source of truth) and
 * docs/api/openapi.base.yaml (the seed's info.version); a Go test fails if
 * the three drift.
 */
export const CONTRACT_VERSION = '1.0.0';

export interface ContractCompatResult {
  /** False only when the daemon's MAJOR contract version is incompatible. */
  ok: boolean;
  /** User-facing message for the blocking refusal screen (set when ok=false). */
  message?: string;
  /** Non-fatal warning (newer minor contract); callers log it. */
  warning?: string;
}

interface ParsedVersion {
  major: number;
  minor: number;
}

/**
 * Parses a "major[.minor[.patch]]" version string. Accepts an optional
 * pre-release suffix ("1.0.0-beta.1") and single-part forms ("1", "1.2").
 * Returns null when the string is not a version at all.
 */
function parseVersion(v: string): ParsedVersion | null {
  const m = /^(\d+)(?:\.(\d+))?(?:\.\d+)?/.exec(v.trim());
  if (!m) return null;
  const major = Number(m[1]);
  const minor = m[2] !== undefined ? Number(m[2]) : 0;
  if (!Number.isFinite(major) || !Number.isFinite(minor)) return null;
  return { major, minor };
}

/**
 * Negotiates the daemon's reported contract version against the expected
 * (default: this build's CONTRACT_VERSION). See the module doc for the
 * rules. A missing/empty reported version (a daemon older than the field)
 * is treated as compatible: the negotiation is opt-in for the daemon.
 */
export function checkContractCompat(
  daemonVersion: string | undefined,
  expected: string = CONTRACT_VERSION,
): ContractCompatResult {
  if (daemonVersion === undefined || daemonVersion.trim() === '') {
    return { ok: true };
  }
  const want = parseVersion(expected);
  const have = parseVersion(daemonVersion);
  if (want === null || have === null) {
    const bad = have === null ? daemonVersion : expected;
    return {
      ok: false,
      message:
        `Incompatible API contract: the reported contract version "${bad}" cannot be parsed. ` +
        `Update the daemon (or rebuild this client against a current contract) and try again.`,
    };
  }
  if (have.major !== want.major) {
    return {
      ok: false,
      message:
        `Incompatible API contract: this client was built against contract version ${expected}, ` +
        `but the daemon reports ${daemonVersion}. Major contract versions are not compatible with ` +
        `each other — update the daemon (or rebuild this client) to a matching major version, ` +
        `then reload.`,
    };
  }
  if (have.minor > want.minor) {
    return {
      ok: true,
      warning:
        `Newer minor API contract: the daemon reports ${daemonVersion} but this client was ` +
        `built against ${expected}; features introduced by the newer contract may be ` +
        `unavailable in this build.`,
    };
  }
  return { ok: true };
}

/**
 * The blocking screen shown when the daemon's major contract version is
 * incompatible: the app cannot safely run against it, so instead of the
 * editor a full-viewport notice explains what happened and how to fix it.
 */
export function ContractRefusal({ message }: { message: string }) {
  return (
    <div
      role="alert"
      style={{
        position: 'fixed',
        inset: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '2rem',
        background: '#101014',
        color: '#f5f5f7',
        fontFamily: 'system-ui, sans-serif',
      }}
    >
      <div style={{ maxWidth: 520, textAlign: 'center' }}>
        <h1 style={{ fontSize: '1.4rem', margin: '0 0 0.75rem' }}>Incompatible API contract version</h1>
        <p style={{ lineHeight: 1.6, color: '#c8c8d0' }}>{message}</p>
      </div>
    </div>
  );
}
