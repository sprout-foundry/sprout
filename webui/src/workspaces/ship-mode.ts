/**
 * Ship mode registration.
 *
 * Ship registers through the public mode API (`registerWorkspaceMode`), not as
 * a built-in like Code and Design. The distinction is deliberate: the built-ins
 * are the app's baseline — the modes the switcher, default resolution, and the
 * surfaces all assume exist, so the app can never render without them. Ship is
 * a lens on the same project conversation, an additional way to look at a
 * workspace that already works without it. Treating it as a built-in would
 * harden a mode that is still settling and put it on the small list the registry
 * refuses to replace or remove; the public API leaves it overridable and
 * removable, which is what an added lens should be.
 *
 * Registration happens at module load, through the same write path an embedding
 * shell would use, so the mode is listed in the switcher for any workspace and a
 * caller can still override or dispose it afterwards.
 */

import { Rocket } from 'lucide-react';
import { registerWorkspaceMode, type UnregisterWorkspaceMode } from './registry';
import ShipShell from './ShipShell';

/**
 * Register the Ship mode and return its disposer. The module calls this at
 * load; the exported function lets a test (or an embedding shell that wants a
 * different Ship definition) register or re-register it explicitly.
 */
export function registerShipMode(): UnregisterWorkspaceMode {
  return registerWorkspaceMode({
    id: 'ship',
    label: 'Ship',
    icon: Rocket,
    hint: 'Deploy status, history, rollback',
    // Ship is offered for every workspace: it shows a not-yet-deployed state
    // rather than hiding, the same way Design shows its empty state.
    available: () => true,
    Shell: ShipShell,
  });
}

export const SHIP_MODE_ID = 'ship' as const;

registerShipMode();
