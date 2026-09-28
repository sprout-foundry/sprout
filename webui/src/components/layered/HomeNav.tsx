/**
 * Layer 2 while Home is open: the platform's places (dashboard, tasks,
 * workspaces) and the account pages, plus the way back to the project.
 */

import {
  ChevronLeft,
  CreditCard,
  LayoutDashboard,
  ListChecks,
  Monitor,
  Server,
  Settings,
  Shield,
  Users,
  type LucideIcon,
} from 'lucide-react';
import type { ReactElement } from 'react';
import { getBootstrapUser } from '../../bootstrapAdapter';
import { useFullWorkspacesAvailable } from '../../services/fullWorkspace';
import { openHome } from '../../services/homeView';

interface HomeEntry {
  path: string;
  label: string;
  icon: LucideIcon;
  /** Other routes that belong to this entry (a task page is under Tasks). */
  also?: string[];
}

const WORK: HomeEntry[] = [
  { path: '/', label: 'Dashboard', icon: LayoutDashboard, also: ['/repos'] },
  { path: '/tasks', label: 'Tasks', icon: ListChecks, also: ['/scheduled'] },
  { path: '/workspaces', label: 'Workspaces', icon: Monitor },
];

const ACCOUNT: HomeEntry[] = [
  { path: '/account/billing', label: 'Usage & billing', icon: CreditCard },
  { path: '/team', label: 'Team', icon: Users },
  { path: '/runners', label: 'Runners', icon: Server },
  { path: '/settings', label: 'Settings', icon: Settings },
];

function isActive(entry: HomeEntry, path: string): boolean {
  const roots = [entry.path, ...(entry.also ?? [])];
  return roots.some((root) => (root === '/' ? path === '/' : path === root || path.startsWith(`${root}/`)));
}

interface HomeNavProps {
  path: string;
  projectLabel: string;
  onBackToProject: () => void;
  /** After a page is chosen (phones close the drawer). */
  onNavigated?: () => void;
}

export default function HomeNav({ path, projectLabel, onBackToProject, onNavigated }: HomeNavProps): ReactElement {
  // Workspaces only where the deployment offers them, as on the platform.
  const workspacesAvailable = useFullWorkspacesAvailable(true);
  const work = workspacesAvailable ? WORK : WORK.filter((e) => e.path !== '/workspaces');
  const account = getBootstrapUser()?.admin ? [...ACCOUNT, { path: '/admin', label: 'Admin', icon: Shield }] : ACCOUNT;
  const item = (entry: HomeEntry) => (
    <button
      key={entry.path}
      type="button"
      className={`project-nav-item${isActive(entry, path) ? ' active' : ''}`}
      onClick={() => {
        openHome(entry.path);
        onNavigated?.();
      }}
    >
      <entry.icon size={15} />
      <span>{entry.label}</span>
    </button>
  );
  return (
    <div className="project-nav" data-testid="home-nav">
      <div className="project-nav-header">
        <span className="project-nav-title">Home</span>
      </div>
      <div className="project-nav-scroll">
        <button type="button" className="project-nav-item project-nav-return" onClick={onBackToProject}>
          <ChevronLeft size={15} />
          <span>Back to {projectLabel}</span>
        </button>
        <div className="project-nav-section">
          <div className="project-nav-heading">
            <span>Work</span>
          </div>
          {work.map(item)}
        </div>
        <div className="project-nav-section">
          <div className="project-nav-heading">
            <span>Account</span>
          </div>
          {account.map(item)}
        </div>
      </div>
    </div>
  );
}
