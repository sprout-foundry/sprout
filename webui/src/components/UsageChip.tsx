/**
 * UsageChip — ambient platform usage signal in the editor header (SP-016 P0.6).
 *
 * Cloud mode only. Fed by the nav contract's badge fields (SP-016 P0.2):
 * the `tasks` item carries the pending/running task count (number badge)
 * and the `billing` item carries the overage state (string badge). The nav
 * contract stays the transport — no new API surface here.
 *
 * Renders nothing when: local mode (no user, no platform), the bootstrap
 * served no badge values (platform data unavailable), or every value is
 * empty (no running tasks, no overage). "Render only when data exists."
 */
import { usePlatformNav } from '../contexts/PlatformNavContext';

function findBadge(items: readonly { id: string; badge?: number | string }[], id: string): number | string | undefined {
  const item = items.find((i) => i.id === id);
  return item?.badge;
}

export function UsageChip() {
  const { platformNavItems } = usePlatformNav();

  const taskBadge = findBadge(platformNavItems, 'tasks');
  const billingBadge = findBadge(platformNavItems, 'billing');

  const hasTasks = typeof taskBadge === 'number' && taskBadge > 0;
  const hasOverage = typeof billingBadge === 'string' && billingBadge.length > 0;

  // No data → no chip (local mode, bootstrap without badges, or clean state).
  if (!hasTasks && !hasOverage) return null;

  const title = hasOverage
    ? 'You are on overage — view billing on the platform'
    : `${taskBadge} task${taskBadge === 1 ? '' : 's'} pending or running on the platform`;

  return (
    <span className="usage-chip" data-testid="header-usage-chip" title={title}>
      {hasOverage ? <span className="usage-chip-segment usage-chip-overage">overage</span> : null}
      {hasOverage && hasTasks ? (
        <span className="usage-chip-sep" aria-hidden="true">
          ·
        </span>
      ) : null}
      {hasTasks ? (
        <span className="usage-chip-segment">
          {taskBadge} task{taskBadge === 1 ? '' : 's'}
        </span>
      ) : null}
    </span>
  );
}
