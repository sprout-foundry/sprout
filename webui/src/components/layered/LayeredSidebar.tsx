/**
 * The layered layout's left side: the project rail (L1) and the project
 * sidebar (L2). Owns only which L2 entry is open; the panels it drills into
 * are the classic sidebar's own sections, rendered by the caller.
 */

import { useState, type ReactElement, type ReactNode } from 'react';
import { isCloud, supportsAutomations, supportsGit, supportsSettings } from '../../config/mode';
import type { SectionTab } from '../../hooks/useSidebarState';
import type { SproutInstance } from '../../services/api';
import { useActiveRepoURL } from '../../services/activeRepo';
import { useRecentRepos } from '../../services/recentRepos';
import { parseRepoRef } from '../../services/workspaceFs/workspaceGit';
import { showThemedAlert, showThemedPrompt } from '../ThemedDialog';
import type { ViewType } from '../../types/app';
import { githubRepoSlug } from '../../utils/platformUrl';
import type { WorkspaceMode, WorkspaceModeId } from '../../workspaces/registry';
import ProjectNav, { NAV_ICONS, type ProjectNavConversations, type ProjectNavTarget } from './ProjectNav';
import ProjectRail, { type RailProject } from './ProjectRail';
import './Layered.css';

const SECTION_TITLES: Record<string, string> = {
  files: 'Files',
  search: 'Search',
  git: 'Source control',
  automations: 'Automations',
  settings: 'Settings',
  logs: 'Logs',
  screens: 'Design',
  tokens: 'Design',
  flows: 'Design',
  feedback: 'Design',
};

function basename(path: string | undefined): string {
  const trimmed = (path ?? '').replace(/\/+$/, '');
  return trimmed.split('/').pop() || trimmed || 'Workspace';
}

export interface LayeredSidebarProps {
  conversations?: ProjectNavConversations;
  currentView?: ViewType;
  onViewChange?: (view: ViewType) => void;
  modes: WorkspaceMode[];
  activeModeId: WorkspaceModeId;
  onSelectMode?: (id: WorkspaceModeId) => void;
  modeSection?: string;
  onModeSectionChange?: (id: string) => void;
  onSectionChange?: (section: SectionTab) => void;
  workspaceRoot?: string;
  instances: SproutInstance[];
  onInstanceChange?: (pid: number) => void;
  /** Renders the classic sidebar panel for the selected section. */
  renderSection: () => ReactNode;
  /** Collapsed: only the rail shows (narrow windows, tablets). */
  collapsed?: boolean;
  onToggleCollapsed?: () => void;
  isMobile?: boolean;
}

// Opening a repository reloads onto ?repo=, the same clone-or-restore path
// a dashboard "Open in editor" link takes.
function openRepo(url: string): void {
  const params = new URLSearchParams(window.location.search);
  if (params.get('repo') === url) return;
  params.set('repo', url);
  window.location.search = params.toString();
}

async function promptForRepo(): Promise<void> {
  const input = await showThemedPrompt('GitHub repository to open (owner/name or URL):', {
    title: 'Open a repository',
    placeholder: 'owner/repo',
  });
  if (!input?.trim()) return;
  try {
    openRepo(parseRepoRef(input.trim()).url.replace(/\.git$/, ''));
  } catch (err) {
    await showThemedAlert(err instanceof Error ? err.message : String(err), {
      title: 'Invalid repository',
      type: 'warning',
    });
  }
}

export default function LayeredSidebar(props: LayeredSidebarProps): ReactElement {
  const [open, setOpen] = useState<ProjectNavTarget | null>(null);
  const activeRepo = useActiveRepoURL();
  const repoSlug = githubRepoSlug(activeRepo);
  const recentRepos = useRecentRepos();
  const title = isCloud ? (repoSlug ?? 'No repository open') : basename(props.workspaceRoot);

  const inCode = props.activeModeId !== 'design';
  const conversationInMain = inCode && props.currentView === 'chat';

  const navigate = (target: ProjectNavTarget) => {
    if (target.kind === 'conversation') {
      if (!inCode) props.onSelectMode?.('code');
      if (target.id) props.conversations?.onSelect(target.id);
      props.onViewChange?.('chat');
      setOpen(null);
      return;
    }
    if (target.kind === 'design') {
      if (inCode) props.onSelectMode?.('design');
      props.onModeSectionChange?.(target.id);
      setOpen(target);
      return;
    }
    const global = target.id === 'settings' || target.id === 'logs';
    if (!global && !inCode) props.onSelectMode?.('code');
    props.onSectionChange?.(target.id as SectionTab);
    setOpen(target);
  };

  const entry = (kind: 'section' | 'design', id: keyof typeof NAV_ICONS, label: string) => ({
    target: { kind, id } as ProjectNavTarget,
    label,
    icon: NAV_ICONS[id],
  });

  const codeEntries = [
    entry('section', 'files', 'Files'),
    entry('section', 'search', 'Search'),
    ...(supportsGit ? [entry('section', 'git', 'Source control')] : []),
  ];
  const designEntries = props.modes.some((m) => m.id === 'design')
    ? [
        entry('design', 'screens', 'Screens'),
        entry('design', 'flows', 'Flows'),
        entry('design', 'tokens', 'Tokens'),
        entry('design', 'feedback', 'Feedback'),
      ]
    : [];
  const automationEntries = supportsAutomations ? [entry('section', 'automations', 'Workflows')] : [];
  const footerEntries = [
    ...(supportsSettings ? [entry('section', 'settings', 'Settings')] : []),
    entry('section', 'logs', 'Logs'),
  ];

  // Design sections render the design assets pane, the same panel the
  // classic sidebar shows beside the design rail; the L2 entries stand in
  // for the rail's icons.
  const drillContent = props.renderSection();

  const projects: RailProject[] = isCloud
    ? recentRepos.map((url) => ({
        id: url,
        label: githubRepoSlug(url) ?? url,
        active: githubRepoSlug(url) === repoSlug,
        onSelect: () => openRepo(url),
      }))
    : props.instances.length > 0
      ? props.instances.map((inst) => ({
          id: String(inst.pid),
          label: basename(inst.working_dir),
          active: inst.is_current,
          onSelect: () => props.onInstanceChange?.(inst.pid),
        }))
      : [{ id: 'local', label: title, active: true }];

  return (
    <>
      <ProjectRail
        projects={projects}
        collapsed={props.collapsed}
        onToggleCollapsed={props.isMobile ? undefined : props.onToggleCollapsed}
        onAddProject={isCloud ? () => void promptForRepo() : undefined}
        onOpenSettings={() => navigate({ kind: 'section', id: 'settings' })}
      />
      {!props.collapsed && (
        <ProjectNav
          title={title}
          onHide={props.onToggleCollapsed}
          hideLabel={props.isMobile ? 'Close sidebar' : 'Collapse sidebar'}
          conversations={props.conversations}
          conversationInMain={conversationInMain && !open}
          current={open}
          onNavigate={navigate}
          codeEntries={codeEntries}
          designEntries={designEntries}
          automationEntries={automationEntries}
          footerEntries={footerEntries}
          drill={
            open
              ? { title: SECTION_TITLES[open.id] ?? open.id, content: drillContent, onBack: () => setOpen(null) }
              : undefined
          }
        />
      )}
    </>
  );
}
