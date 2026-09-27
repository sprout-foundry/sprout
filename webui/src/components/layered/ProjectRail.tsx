/**
 * Layer 1 of the layered layout: where you are across projects. Home and
 * Activity sit above the project list; the account sits at the bottom. In
 * the hosted editor "projects" are repositories; on a local daemon they are
 * the running workspaces.
 */

import { Bell, FolderGit2, Home, Plus, Settings } from 'lucide-react';
import type { ReactElement } from 'react';
import { isCloud } from '../../config/mode';
import { OPEN_NOTIFICATIONS_EVENT } from '../../config/layout';
import { platformHref } from '../../utils/platformUrl';
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
}

function initial(label: string): string {
  const name = label.split('/').pop() || label;
  return name.charAt(0).toUpperCase();
}

export default function ProjectRail({ projects, onAddProject, onOpenSettings }: ProjectRailProps): ReactElement {
  return (
    <nav className="project-rail" aria-label="Projects" data-testid="project-rail">
      {isCloud ? (
        <a className="project-rail-btn" href={platformHref('/?from=editor')} title="Home — all projects">
          <Home size={18} />
        </a>
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
        onClick={() => window.dispatchEvent(new Event(OPEN_NOTIFICATIONS_EVENT))}
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
