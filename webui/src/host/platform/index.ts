/**
 * The platform implementation — INTERNAL.
 *
 * This module is the cloud host's platform surface: the concrete platform
 * pages, URL resolution, GitHub-through-the-account helpers, entitlements and
 * the account card. It is NOT part of the host contract and is deliberately
 * NOT re-exported from the public host entry (`host/index.ts`): a component
 * outside the host tree must not import from here, only from the contract
 * (`host/index`). The app entry (`webui/src/index.tsx`) imports `cloudHost`
 * from here to select the hosted host at startup.
 *
 * The public entry carries the contract (types, provider, hooks, accessor,
 * `localHost`, the host-agnostic repo-naming helpers). Everything platform-
 * specific stays behind this module.
 */

export { cloudHost } from '../cloudHost';
export {
  PLATFORM_ACCOUNT_ITEMS,
  PLATFORM_ADMIN_ITEM,
  PLATFORM_WORK_ITEMS,
  intentPath,
  itemHref,
  platformEntitlements,
  platformEmbedPagePath,
  platformPagePath,
} from './pages';
export { CLOUD_NAV_ITEMS } from '../platformNav';
export { platformHref, repoHubPath, platformEmbedPath } from '../platformUrl';
export {
  fetchPlatformGitHubConnected,
  listPlatformRepos,
  createPlatformRepo,
  usesPlatformGitHub,
  platformGitHubSettingsHref,
  CreateRepoError,
} from '../platformGitHub';
export { default as PlatformGitHubAccountCard } from '../PlatformGitHubAccountCard';
