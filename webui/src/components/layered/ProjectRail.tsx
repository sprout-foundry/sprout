/**
 * Layer 1 of the layered layout: where you are across projects. Home and
 * Activity sit above the project list; the account sits at the bottom. In
 * the hosted editor "projects" are repositories; on a local daemon they are
 * the running workspaces.
 */

import { Bell, FolderGit2, Home, PanelLeftOpen, Plus, Settings } from 'lucide-react';
import type { ReactElement } from 'react';
import { isCloud } from '../../config/mode';
import { OPEN_NOTIFICATIONS_EVENT } from '../../config/layout';
import { UserMenu } from '../UserMenu';

export interface RailProject {
  id: string;
  label: string;
  active: boolean;
  onSelect?: () => void;
}

interface ProjectRailProps {
  projects: RailProject[];
  onAddProject?: () => void;
  onOpenSettings?: () => void;
  collapsed?: boolean;
  onToggleCollapsed?: () => void;
  /** Hosted: Home opens inside the shell. */
  homeActive?: boolean;
  onOpenHome?: () => void;
}

function initial(label: string): string {
  const name = label.split('/').pop() || label;
  return name.charAt(0).toUpperCase();
}

export default function ProjectRail({
  projects,
  onAddProject,
  onOpenSettings,
  collapsed,
  onToggleCollapsed,
  homeActive,
  onOpenHome,
}: ProjectRailProps): ReactElement {
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
      <button
        type="button"
        className="project-rail-btn"
        title="Activity"
        aria-label="Activity"
        onClick={(e) =>
          window.dispatchEvent(new CustomEvent(OPEN_NOTIFICATIONS_EVENT, { detail: { anchor: e.currentTarget } }))
        }
      >
        <Bell size={18} />
      </button>
      <div className="project-rail-divider" role="separator" />
      {projects.map((p) => (
        <button
          key={p.id}
          type="button"
          className={`project-rail-project${p.active ? ' active' : ''}`}
          title={p.label}
          aria-label={p.label}
          aria-current={p.active ? 'true' : undefined}
          onClick={p.onSelect}
        >
          {initial(p.label)}
        </button>
      ))}
      {onAddProject && (
        <button
          type="button"
          className="project-rail-btn"
          title="Open another project"
          aria-label="Open another project"
          onClick={onAddProject}
        >
          <Plus size={18} />
        </button>
      )}
      <div className="project-rail-spacer" />
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
