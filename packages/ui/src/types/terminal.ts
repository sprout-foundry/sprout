/**
 * Terminal types for @sprout/ui.
 *
 * These types define core data structures used for terminal management
 * across the UI. They are the single canonical source — consumers in both
 * `packages/ui` and `webui` should import from here (via `@sprout/ui`).
 */

/** Information about an available shell on the system */
export interface ShellInfo {
  name: string;
  path: string;
  default: boolean;
}
