/**
 * Layer 1 of the layered layout: where you are across projects. Home sits
 * above the project list; Activity and the account sit at the bottom. In
 * the hosted editor "projects" are repositories; on a local daemon they are
 * the running workspaces.
 */

import { Bell, FolderGit2, Home, PanelLeftOpen, Plus, Settings } from 'lucide-react';
import { useState, type ReactElement } from 'react';
import { isCloud } from '../../config/mode';
import { OPEN_NOTIFICATIONS_EVENT } from '../../config/layout';
import { UserMenu } from '../UserMenu';

export interface RailProject {
  id: string;
  label: string;
  active: boolean;
  onSelect?: () => void;
}

export interface RailAddAction {
  label: string;
  onSelect: () => void;
}

interface ProjectRailProps {
  projects: RailProject[];
  /** What "+" offers; a single action runs directly, several open a menu. */
  addActions?: RailAddAction[];
  onOpenSettings?: () => void;
  collapsed?: boolean;
  onToggleCollapsed?: () => void;
  /** Hosted: Home opens inside the shell. */
  homeActive?: boolean;
  onOpenHome?: () => void;
  /** Runs after Activity opens (phones close the drawer the rail sits in). */
  onOpenActivity?: () => void;
}

/**
 * A project's monogram, read like an element symbol: a large first letter and
 * a small second one — the next word's initial for multi-word names
 * (sprout-foundry → Sf), otherwise the name's second letter (oauth → Oa) — so
 * projects that share a first letter still tell apart.
 */
export function monogram(label: string): { major: string; minor: string } {
  const name = label.split('/').pop() || label;
  const words = name
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .split(/[\s._-]+/)
    .filter(Boolean);
  const major = (words[0]?.[0] ?? '?').toUpperCase();
  const minor = words.length > 1 ? words[1][0] : (words[0]?.replace(/[^A-Za-z0-9]/g, '')[1] ?? '');
  return { major, minor: minor.toLowerCase() };
}

/** The full name beside a rail tile while it's hovered or focused. */
function RailHoverCard({ label, anchor }: { label: string; anchor: HTMLElement }): ReactElement {
  const rect = anchor.getBoundingClientRect();
  const slash = label.lastIndexOf('/');
  return (
    <div className="rail-hover-card" role="tooltip" style={{ left: rect.right + 8, top: rect.top + rect.height / 2 }}>
      {slash > 0 && <span className="rail-hover-card-owner">{label.slice(0, slash + 1)}</span>}
      <span className="rail-hover-card-name">{label.slice(slash + 1)}</span>
    </div>
  );
}

export default function ProjectRail({
  projects,
  addActions,
  onOpenSettings,
  collapsed,
  onToggleCollapsed,
  homeActive,
  onOpenHome,
  onOpenActivity,
}: ProjectRailProps): ReactElement {
  const [hovered, setHovered] = useState<{ label: string; anchor: HTMLElement } | null>(null);
  const [addMenu, setAddMenu] = useState<DOMRect | null>(null);
  return (
    <nav className="project-rail" aria-label="Projects" data-testid="project-rail">
      {collapsed && onToggleCollapsed && (
        <button
          type="button"
          className="project-rail-btn"
          title="Show project sidebar"
          aria-label="Show project sidebar"
          onClick={onToggleCollapsed}
        >
          <PanelLeftOpen size={18} />
        </button>
      )}
      {isCloud ? (
        <button
          type="button"
          className={`project-rail-btn${homeActive ? ' active' : ''}`}
          title="Home — dashboard, tasks, account"
          aria-label="Home"
          aria-current={homeActive ? 'page' : undefined}
          onClick={onOpenHome}
        >
          <Home size={18} />
        </button>
      ) : (
        <span className="project-rail-btn project-rail-brand" aria-hidden="true">
          <FolderGit2 size={18} />
        </span>
      )}
      <div className="project-rail-divider" role="separator" />
      {projects.map((p) => {
        const mono = monogram(p.label);
        const show = (e: { currentTarget: HTMLElement }) => setHovered({ label: p.label, anchor: e.currentTarget });
        return (
          <button
            key={p.id}
            type="button"
            className={`project-rail-project${p.active ? ' active' : ''}`}
            aria-label={p.label}
            aria-current={p.active ? 'true' : undefined}
            onClick={p.onSelect}
            onMouseEnter={show}
            onFocus={show}
            onMouseLeave={() => setHovered(null)}
            onBlur={() => setHovered(null)}
          >
            <span className="project-rail-mono-major">{mono.major}</span>
            <span className="project-rail-mono-minor">{mono.minor}</span>
          </button>
        );
      })}
      {hovered && <RailHoverCard label={hovered.label} anchor={hovered.anchor} />}
      {addActions && addActions.length > 0 && (
        <button
          type="button"
          className="project-rail-btn"
          title="Add a project"
          aria-label="Add a project"
          aria-haspopup={addActions.length > 1 ? 'menu' : undefined}
          aria-expanded={addActions.length > 1 ? addMenu !== null : undefined}
          onClick={(e) => {
            if (addActions.length === 1) addActions[0].onSelect();
            else setAddMenu(addMenu ? null : e.currentTarget.getBoundingClientRect());
          }}
        >
          <Plus size={18} />
        </button>
      )}
      {addMenu && addActions && (
        <>
          <div className="user-menu-backdrop" onClick={() => setAddMenu(null)} aria-hidden="true" />
          <div
            className="user-menu-list"
            role="menu"
            aria-label="Add a project"
            style={{ position: 'fixed', left: addMenu.right + 8, top: addMenu.top, right: 'auto' }}
            onKeyDown={(e) => e.key === 'Escape' && setAddMenu(null)}
          >
            {addActions.map((action, i) => (
              <button
                key={action.label}
                type="button"
                role="menuitem"
                className="user-menu-item"
                autoFocus={i === 0}
                onClick={() => {
                  setAddMenu(null);
                  action.onSelect();
                }}
              >
                {action.label}
              </button>
            ))}
          </div>
        </>
      )}
      <div className="project-rail-spacer" />
      <button
        type="button"
        className="project-rail-btn"
        title="Activity"
        aria-label="Activity"
        onClick={(e) => {
          window.dispatchEvent(new CustomEvent(OPEN_NOTIFICATIONS_EVENT, { detail: { anchor: e.currentTarget } }));
          onOpenActivity?.();
        }}
      >
        <Bell size={18} />
      </button>
      {isCloud ? (
        <div className="project-rail-account">
          <UserMenu />
        </div>
      ) : (
        <button
          type="button"
          className="project-rail-btn"
          title="Settings"
          aria-label="Settings"
          onClick={onOpenSettings}
        >
          <Settings size={18} />
        </button>
      )}
    </nav>
  );
}
