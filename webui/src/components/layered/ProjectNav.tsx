/**
 * Layer 2 of the layered layout: everything in the current project, grouped
 * by what you are doing — conversations with the agent, code, design, and
 * automations. Picking a tool drills into its existing panel (file tree,
 * source control, search…) with a back arrow, so the panels keep their
 * sidebar home and the list stays one level deep.
 */

import {
  ChevronLeft,
  PanelLeftClose,
  FileCode2,
  FolderTree,
  GitBranch,
  Layers,
  MessageSquare,
  Palette,
  PenSquare,
  ScrollText,
  Search,
  Settings,
  SwatchBook,
  Zap,
  type LucideIcon,
} from 'lucide-react';
import type { ReactElement, ReactNode } from 'react';
import type { ChatSession } from '../../services/chatSessions';

export type ProjectNavTarget =
  | { kind: 'conversation'; id: string }
  | { kind: 'section'; id: string }
  | { kind: 'design'; id: string };

export interface ProjectNavConversations {
  sessions: ChatSession[];
  activeId: string | null;
  /** True while the conversation is the main view's focused tab. */
  inMain: boolean;
  onSelect: (id: string) => void;
  /** Brings the conversation into the main view. */
  onOpen: () => void;
  onCreate?: () => void;
}

interface NavEntry {
  target: ProjectNavTarget;
  label: string;
  icon: LucideIcon;
}

interface ProjectNavProps {
  title: string;
  onHide?: () => void;
  hideLabel?: string;
  conversations?: ProjectNavConversations;
  /** True while a conversation fills the main view. */
  conversationInMain: boolean;
  current: ProjectNavTarget | null;
  onNavigate: (target: ProjectNavTarget) => void;
  codeEntries: NavEntry[];
  designEntries: NavEntry[];
  automationEntries: NavEntry[];
  footerEntries: NavEntry[];
  /** When set, the nav shows this panel instead of the list. */
  drill?: { title: string; content: ReactNode; onBack: () => void };
}

function same(a: ProjectNavTarget | null, b: ProjectNavTarget): boolean {
  return !!a && a.kind === b.kind && a.id === b.id;
}

function Section({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <div className="project-nav-section">
      <div className="project-nav-heading">
        <span>{title}</span>
        {action}
      </div>
      {children}
    </div>
  );
}

export default function ProjectNav(props: ProjectNavProps): ReactElement {
  const { title, conversations, conversationInMain, current, onNavigate, drill } = props;

  const hideButton = props.onHide ? (
    <button
      type="button"
      className="project-nav-back"
      onClick={props.onHide}
      title={props.hideLabel}
      aria-label={props.hideLabel}
    >
      <PanelLeftClose size={15} />
    </button>
  ) : null;

  if (drill) {
    return (
      <div className="project-nav" data-testid="project-nav">
        <div className="project-nav-drill-header">
          <button type="button" className="project-nav-back" onClick={drill.onBack} aria-label="Back to project">
            <ChevronLeft size={16} />
          </button>
          <span className="project-nav-drill-title">{drill.title}</span>
          {hideButton}
        </div>
        <div className="project-nav-drill-body content-pane-scroll">{drill.content}</div>
      </div>
    );
  }

  const item = (entry: NavEntry) => (
    <button
      key={`${entry.target.kind}:${entry.target.id}`}
      type="button"
      className={`project-nav-item${same(current, entry.target) ? ' active' : ''}`}
      onClick={() => onNavigate(entry.target)}
    >
      <entry.icon size={15} />
      <span>{entry.label}</span>
    </button>
  );

  const sessions = (conversations?.sessions ?? []).filter((s) => (s.mode ?? 'code') !== 'design' || s.is_active);

  return (
    <div className="project-nav" data-testid="project-nav">
      <div className="project-nav-header">
        <span className="project-nav-title" title={title}>
          {title}
        </span>
        {hideButton}
        {conversations?.onCreate && (
          <button type="button" className="project-nav-new" onClick={conversations.onCreate} title="New conversation">
            <PenSquare size={14} /> New
          </button>
        )}
      </div>
      <div className="project-nav-scroll">
        <Section title="Conversations">
          {sessions.length === 0 && (
            // Hosts that keep a single implicit conversation list none; it
            // still exists and opens in the main view.
            <button
              type="button"
              className={`project-nav-item${conversationInMain ? ' active' : ''}`}
              onClick={() => onNavigate({ kind: 'conversation', id: conversations?.activeId ?? '' })}
            >
              <MessageSquare size={15} />
              <span>Conversation</span>
            </button>
          )}
          {sessions.map((s) => (
            <button
              key={s.id}
              type="button"
              className={`project-nav-item${conversationInMain && s.id === conversations?.activeId ? ' active' : ''}`}
              onClick={() => onNavigate({ kind: 'conversation', id: s.id })}
              title={s.name}
            >
              <MessageSquare size={15} />
              <span>{s.name || 'Untitled'}</span>
              {s.active_query && <span className="project-nav-running" title="Working" />}
            </button>
          ))}
        </Section>
        <Section title="Code">{props.codeEntries.map(item)}</Section>
        {props.designEntries.length > 0 && <Section title="Design">{props.designEntries.map(item)}</Section>}
        {props.automationEntries.length > 0 && (
          <Section title="Automations">{props.automationEntries.map(item)}</Section>
        )}
      </div>
      <div className="project-nav-footer">{props.footerEntries.map(item)}</div>
    </div>
  );
}

export const NAV_ICONS = {
  files: FolderTree,
  search: Search,
  git: GitBranch,
  automations: Zap,
  settings: Settings,
  logs: ScrollText,
  screens: FileCode2,
  tokens: SwatchBook,
  flows: Layers,
  feedback: MessageSquare,
  design: Palette,
};
