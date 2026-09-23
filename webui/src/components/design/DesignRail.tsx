/**
 * Design mode's rail — screen-centric IA (SP-140-8 §8a, item 8.1).
 *
 * Two groups, rendered in the sidebar's icon-rail column:
 *   Screens  — the primary axis: one entry per wireframe stem from the
 *              inventory. Each entry is an icon button carrying the README
 *              status marker (draft / review / ready) as a colored dot and the
 *              open-annotation count (from design/feedback/<stem>.json) as a
 *              count badge. Selecting a screen drives the shared workspace
 *              selection and lands on the Screens section (the screen
 *              workbench itself is item 8.2).
 *   Library  — the kind views (Tokens, Flows) repositioned as secondary,
 *              global surfaces. Behavior is unchanged; only their position.
 *
 * Status source: the data layer (designApi.listAssets) populates `status` on
 * the `screens` inventory (the README manifest is keyed by screen stem), not
 * on wireframes. So a wireframe stem's status is looked up from the
 * `screens` entry of the same stem, with a wireframe's own `status` as a
 * fallback if a future data layer populates it.
 *
 * The rail is data-driven from the design workspace inventory. When no
 * workspace context is present (standalone render, tests) or the inventory
 * has no wireframes, the Screens group omits itself and only the Library
 * group renders — the rail never crashes on an empty tree.
 *
 * Deliberately does NOT carry the Code rail's entries (Git/Files/Search) — a
 * mode's rail lists what that mode does.
 */

import { FileText, Layers, Palette, type LucideIcon } from 'lucide-react';
import React, { useMemo } from 'react';
import type { ModeRailProps } from '../../workspaces/rail';
import type { DesignTab } from './DesignView';
import { useDesignWorkspace } from './DesignWorkspaceContext';
import './DesignRail.css';

/** Strip the trailing extension from an asset name to its stem. */
function stemOf(name: string): string {
  return name.replace(/\.[^.]+$/, '');
}

/** The Library group: the kind views, repositioned as secondary surfaces. */
const LIBRARY_ENTRIES: {
  id: DesignTab;
  label: string;
  icon: LucideIcon;
}[] = [
  { id: 'tokens', label: 'Tokens', icon: Palette },
  { id: 'flows', label: 'Flows', icon: Layers },
];

/**
 * README status markers the rail renders a dot for. Anything outside this set
 * is ignored (no dot) so a future data layer can't forge an arbitrary
 * `design-rail-status--*` class.
 */
const KNOWN_STATUSES = new Set(['draft', 'review', 'ready']);

export default function DesignRail({ activeId, onSelect }: ModeRailProps) {
  const workspace = useDesignWorkspace();
  const wireframes = useMemo(
    () => workspace?.inventory?.wireframes ?? [],
    [workspace?.inventory?.wireframes],
  );
  const screens = useMemo(
    () => workspace?.inventory?.screens ?? [],
    [workspace?.inventory?.screens],
  );
  const feedback = useMemo(
    () => workspace?.inventory?.feedback ?? [],
    [workspace?.inventory?.feedback],
  );
  const selected = workspace?.selected ?? null;

  // Open-annotation count per wireframe stem. A feedback file is keyed
  // design/feedback/<stem>.json, so its stem is the name minus the
  // extension — the same stem as a wireframe's.
  const openByStem = useMemo(() => {
    const map = new Map<string, number>();
    for (const f of feedback) {
      const open = f.annotationCount - f.resolvedCount;
      if (open > 0) {
        map.set(stemOf(f.name), open);
      }
    }
    return map;
  }, [feedback]);

  // README status per stem, sourced from the `screens` inventory (the data
  // layer populates `status` there, keyed by lowercased screen stem). A
  // wireframe stem's dot falls back to its own `status` if ever set.
  const statusByStem = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of screens) {
      if (s.status) {
        map.set(stemOf(s.name).toLowerCase(), s.status);
      }
    }
    return map;
  }, [screens]);

  const selectScreen = (path: string) => {
    workspace?.select(path);
    onSelect('screens');
  };

  const showScreens = wireframes.length > 0;

  return (
    <nav aria-label="Design navigation" className="design-rail" data-testid="design-rail">
      {showScreens && (
        <div
          className="design-rail-group"
          role="tablist"
          aria-orientation="vertical"
          aria-label="Screens"
          data-testid="design-rail-screens"
        >
          {wireframes.map((wf) => {
            const stem = stemOf(wf.name);
            const rawStatus = wf.status || statusByStem.get(stem.toLowerCase()) || '';
            const status = KNOWN_STATUSES.has(rawStatus) ? rawStatus : '';
            const open = openByStem.get(stem) ?? 0;
            const isActive = activeId === 'screens' && selected === wf.path;
            const title = [stem, status, open > 0 ? `${open} open` : ''].filter(Boolean).join(' · ');
            return (
              <button
                key={wf.path}
                type="button"
                role="tab"
                aria-selected={isActive}
                aria-label={title}
                title={title}
                className={`rail-icon design-rail-screen ${isActive ? 'active' : ''}`}
                onClick={() => selectScreen(wf.path)}
                data-testid={`design-rail-screen-${stem}`}
              >
                <FileText size={18} strokeWidth={1.5} />
                {status ? (
                  <span
                    className={`design-rail-status design-rail-status--${status}`}
                    data-status={status}
                    aria-hidden="true"
                  />
                ) : null}
                {open > 0 ? (
                  <span className="design-rail-badge" data-open-count={open} aria-hidden="true">
                    {open}
                  </span>
                ) : null}
              </button>
            );
          })}
        </div>
      )}

      {showScreens ? (
        <div className="sidebar-icon-rail-divider" role="separator" aria-label="Library" />
      ) : null}

      <div
        className="design-rail-group"
        role="tablist"
        aria-orientation="vertical"
        aria-label="Library"
        data-testid="design-rail-library"
      >
        {LIBRARY_ENTRIES.map((entry) => {
          const Icon = entry.icon;
          const active = activeId === entry.id;
          return (
            <button
              key={entry.id}
              type="button"
              role="tab"
              aria-selected={active}
              aria-label={entry.label}
              title={entry.label}
              className={`rail-icon ${active ? 'active' : ''}`}
              onClick={() => onSelect(entry.id)}
              data-testid={`design-rail-${entry.id}`}
            >
              <Icon size={18} strokeWidth={1.5} />
            </button>
          );
        })}
      </div>
    </nav>
  );
}
