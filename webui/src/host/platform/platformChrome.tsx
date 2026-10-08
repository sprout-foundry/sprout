/**
 * The cloud host's platform-supplied chrome: the React nodes the platform
 * fills into Sprout's own surfaces. Kept in a `.tsx` module so the host
 * constant (a plain `.ts` value object) stays JSX-free.
 *
 * The GitHub account card is the one chrome surface the platform supplies
 * today: the platform manages the account's GitHub connection, so Sprout
 * renders the host's card wherever it shows a GitHub account panel.
 */

import PlatformGitHubAccountCard from '../PlatformGitHubAccountCard';
import type { HostChrome } from '../types';

export const platformChrome: HostChrome = {
  githubAccount: <PlatformGitHubAccountCard />,
};
