import { version } from '../package.json';

/**
 * The package name and version a host reports when it negotiates a backend
 * contract version with `SproutSpaces`. The version is the package's own, so
 * a host cannot disagree with what it actually loaded.
 */
export const SPACES_PACKAGE_NAME = '@sprout-foundry/spaces';
export const SPACES_PACKAGE_VERSION = version;
