/**
 * Layer 2 while Home is open: the host's work places (dashboard, tasks,
 * workspaces) and account pages, plus the way back to the project.
 *
 * The item lists come from the host (`host.navigation.workItems` /
 * `accountItems`): the host owns the labels and each item's destination, and
 * Sprout renders the items and dispatches their intents through
 * `host.navigation`. No platform page name lives in this component.
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
import { copy, formatCopy } from '../../config/copy';
import type { HostNavItem } from '../../host/types';
import { useHost } from '../../host/useHost';
import { useFullWorkspacesAvailable } from '../../services/fullWorkspace';
import { homeRoute, normalizeHomePath } from '../../services/homeView';

/** An icon for a host-provided item, chosen by its label (icons are Sprout's). */
const ICONS: Record<string, LucideIcon> = {
  Dashboard: LayoutDashboard,
  Tasks: ListChecks,
  Workspaces: Monitor,
  'Usage & billing': CreditCard,
  Team: Users,
  Runners: Server,
  Settings: Settings,
  Admin: Shield,
};

function iconFor(label: string): LucideIcon {
  return ICONS[label] ?? LayoutDashboard;
}

/** The Home route a host-resolved path names (a link's SPA route in its hash). */
function routeOf(path: string): string {
  return homeRoute(normalizeHomePath(path));
}

/** Whether the current Home path belongs to the resolved item path. */
function isActive(itemPath: string, currentPath: string): boolean {
  const root = routeOf(itemPath);
  const route = routeOf(currentPath);
  return root === '/' ? route === '/' : route === root || route.startsWith(`${root}/`);
}

/**
 * The Home page a route belongs to, by its nav label ("Tasks" for a task).
 * Pure: takes the host's nav items and intent resolver so it can be called
 * outside React (the embedded frame's mobile bar) and in tests. The fallback
 * labels the reserved admin area.
 */
export function homePageLabel(
  path: string,
  items: HostNavItem[] = [],
  resolve: (intent: HostNavItem['intent']) => string | null = () => null,
): string {
  const route = routeOf(path);
  for (const item of items) {
    const itemPath = resolve(item.intent);
    if (!itemPath) continue;
    const root = routeOf(itemPath);
    if (root === '/' ? route === '/' : route === root || route.startsWith(`${root}/`)) return item.label;
  }
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
  const { navigation } = useHost();
  const user = getBootstrapUser();
  // Workspaces only where the deployment offers them, as on the platform.
  const workspacesAvailable = useFullWorkspacesAvailable(true);
  const work = (navigation.workItems ?? []).filter((i) => workspacesAvailable || i.label !== 'Workspaces');
  const baseAccount = navigation.accountItems ?? [];
  // Admins get the host's Admin item when it supplies one; otherwise Sprout
  // adds a generic admin entry (the host resolves its destination).
  const hasAdmin = baseAccount.some((i) => i.label === 'Admin');
  const account =
    user?.admin && !hasAdmin
      ? [...baseAccount, { label: 'Admin', intent: { type: 'nav' as const, id: 'admin' } }]
      : baseAccount;

  const item = (entry: HostNavItem) => {
    const itemPath = navigation.intentPath?.(entry.intent);
    const active = itemPath ? isActive(itemPath, path) : false;
    const Icon = iconFor(entry.label);
    return (
      <button
        key={entry.label}
        type="button"
        className={`project-nav-item${active ? ' active' : ''}`}
        onClick={() => {
          navigation.open(entry.intent);
          onNavigated?.();
        }}
      >
        <Icon size={15} />
        <span>{entry.label}</span>
      </button>
    );
  };

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
