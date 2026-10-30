import { copyOverride } from '../config/copy';
import type { SproutHost } from './types';

/**
 * Sprout's own product name, used when a host declares none. This is the name
 * the open-source build shows everywhere; a host that embeds Sprout under its
 * own brand supplies `host.branding.name` instead.
 */
export const SPROUT_PRODUCT_NAME = 'Sprout';

/**
 * The product name to show in Sprout's own surfaces (tab title, install
 * prompt, settings copy). Resolution order:
 *
 *   1. the host's declared brand (`host.branding.name`),
 *   2. an `app.name` copy override installed by an embedding (`installCopy`),
 *   3. Sprout's own name.
 *
 * The copy registry and the host contract are the two ways an embedding can
 * reword Sprout; resolving them in one place keeps them from disagreeing. Only
 * an *installed* `app.name` counts here — its shipped default is the lowercase
 * prose form, not the display name. A blank declaration at either level falls
 * through, so a host cannot accidentally render an empty name.
 */
export function productName(host: SproutHost | null | undefined): string {
  const declared = host?.branding?.name?.trim();
  if (declared) return declared;

  const installed = copyOverride('app.name')?.trim();
  if (installed) return installed;

  return SPROUT_PRODUCT_NAME;
}
