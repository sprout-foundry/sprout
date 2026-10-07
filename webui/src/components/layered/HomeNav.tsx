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
import { copy, formatCopy, type CopyKey } from '../../config/copy';
import { useFullWorkspacesAvailable } from '../../services/fullWorkspace';
import { homeRoute, openHome } from '../../services/homeView';

interface HomeEntry {
  path: string;
  labelKey: CopyKey;
  icon: LucideIcon;
  /** Other routes that belong to this entry (a task page is under Tasks). */
  also?: string[];
}

const WORK: HomeEntry[] = [
  { path: '/', labelKey: 'home.dashboard', icon: LayoutDashboard, also: ['/repos'] },
  { path: '/tasks', labelKey: 'home.tasks', icon: ListChecks, also: ['/scheduled'] },
  { path: '/workspaces', labelKey: 'home.workspaces', icon: Monitor },
];

const ACCOUNT: HomeEntry[] = [
  { path: '/account/billing', labelKey: 'home.billing', icon: CreditCard },
  { path: '/team', labelKey: 'home.team', icon: Users },
  { path: '/runners', labelKey: 'home.runners', icon: Server },
  { path: '/settings', labelKey: 'home.settings', icon: Settings },
];

function isActive(entry: HomeEntry, fullPath: string): boolean {
  const path = homeRoute(fullPath);
  const roots = [entry.path, ...(entry.also ?? [])];
  return roots.some((root) => (root === '/' ? path === '/' : path === root || path.startsWith(`${root}/`)));
}

/** The Home page a route belongs to, by its nav label ("Tasks" for a task). */
export function homePageLabel(path: string): string {
  const entry = [...WORK, ...ACCOUNT].find((e) => isActive(e, path));
  if (entry) return copy(entry.labelKey);
  const route = homeRoute(path);
  return route === '/admin' || route.startsWith('/admin/') ? copy('home.admin') : copy('home.title');
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
  const account = getBootstrapUser()?.admin
    ? [...ACCOUNT, { path: '/admin', labelKey: 'home.admin' as const, icon: Shield }]
    : ACCOUNT;
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
      <span>{copy(entry.labelKey)}</span>
    </button>
  );
  return (
    <div className="project-nav" data-testid="home-nav">
      <div className="project-nav-header">
        <span className="project-nav-title">{copy('home.title')}</span>
      </div>
      <div className="project-nav-scroll">
        <button type="button" className="project-nav-item project-nav-return" onClick={onBackToProject}>
          <ChevronLeft size={15} />
          <span>{formatCopy('home.backToProject', { project: projectLabel })}</span>
        </button>
        <div className="project-nav-section">
          <div className="project-nav-heading">
            <span>{copy('home.work')}</span>
          </div>
          {work.map(item)}
        </div>
        <div className="project-nav-section">
          <div className="project-nav-heading">
            <span>{copy('home.account')}</span>
          </div>
          {account.map(item)}
        </div>
      </div>
    </div>
  );
}
