/**
 * Phones in the hosted editor: the layered layout's places as a bottom tab
 * bar — Home, the open project, Activity, Search and the account — so the
 * drawer is only needed for the project's own sections. Hidden while the
 * keyboard is up, which needs the room.
 */

import { Bell, FolderGit2, Home, Search } from 'lucide-react';
import { useEffect, useState, type ReactElement } from 'react';
import { OPEN_COMMAND_PALETTE_EVENT, OPEN_NOTIFICATIONS_EVENT } from '../../config/layout';
import { useActiveRepoURL } from '../../services/activeRepo';
import { closeHome, openHome, useHomeView } from '../../services/homeView';
import { repoName, repoSlug as repoSlugFromURL } from '../../utils/platformUrl';
import { useUnreadNotificationCount } from '../../hooks/useUnreadNotificationCount';
import { UserMenu } from '../UserMenu';

const ROOT_CLASS = 'phone-tab-bar-shown';

// Any smaller and it's browser chrome (the address bar collapsing), not a keyboard.
const KEYBOARD_MIN_HEIGHT = 150;

// Focus alone doesn't mean a keyboard (the composer takes focus without
// one); the visual viewport shrinking under it does.
function useKeyboardOpen(): boolean {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const viewport = window.visualViewport;
    if (!viewport) return undefined;
    const update = () => setOpen(window.innerHeight - viewport.height > KEYBOARD_MIN_HEIGHT);
    update();
    viewport.addEventListener('resize', update);
    return () => viewport.removeEventListener('resize', update);
  }, []);
  return open;
}

interface PhoneTabBarProps {
  drawerOpen: boolean;
  onToggleDrawer: () => void;
  onCloseDrawer: () => void;
  /** The terminal is open; on a phone it is full screen. */
  terminalOpen: boolean;
  /** Closes the terminal, which would cover the place a tab opens. */
  onLeaveTerminal: () => void;
}

export default function PhoneTabBar({
  drawerOpen,
  onToggleDrawer,
  onCloseDrawer,
  terminalOpen,
  onLeaveTerminal,
}: PhoneTabBarProps): ReactElement | null {
  const home = useHomeView();
  const activeRepoURL = useActiveRepoURL();
  const repoSlug = repoSlugFromURL(activeRepoURL);
  const projectLabel = repoName(activeRepoURL) ?? 'Project';
  const shown = !useKeyboardOpen();
  const unread = useUnreadNotificationCount();

  // The bottom inset every other bottom-anchored surface (terminal, drawer,
  // main view) reserves for the bar.
  useEffect(() => {
    document.documentElement.classList.toggle(ROOT_CLASS, shown);
    return () => document.documentElement.classList.remove(ROOT_CLASS);
  }, [shown]);

  if (!shown) return null;

  const closeDrawer = () => {
    if (drawerOpen) onCloseDrawer();
  };

  return (
    <nav className="phone-tab-bar" aria-label="Places" data-testid="phone-tab-bar">
      <button
        type="button"
        className={`phone-tab${home.open ? ' active' : ''}`}
        aria-current={home.open ? 'page' : undefined}
        onClick={() => {
          openHome(home.path);
          closeDrawer();
          if (terminalOpen) onLeaveTerminal();
        }}
      >
        <Home size={20} aria-hidden="true" />
        <span>Home</span>
      </button>
      <button
        type="button"
        className={`phone-tab${home.open ? '' : ' active'}`}
        aria-current={home.open ? undefined : 'page'}
        title={repoSlug ?? undefined}
        // Tapping the project you're already looking at opens its sections.
        onClick={() => {
          if (home.open || terminalOpen) {
            closeHome();
            closeDrawer();
            if (terminalOpen) onLeaveTerminal();
          } else {
            onToggleDrawer();
          }
        }}
      >
        <FolderGit2 size={20} aria-hidden="true" />
        <span>{projectLabel}</span>
      </button>
      <button
        type="button"
        className="phone-tab"
        onClick={(e) => {
          closeDrawer();
          window.dispatchEvent(new CustomEvent(OPEN_NOTIFICATIONS_EVENT, { detail: { anchor: e.currentTarget } }));
        }}
      >
        <Bell size={20} aria-hidden="true" />
        {unread > 0 && <span className="activity-badge">{unread > 99 ? '99+' : unread}</span>}
        <span>Activity</span>
      </button>
      <button
        type="button"
        className="phone-tab"
        // Search finds the project's files and commands, so it shows the project.
        onClick={() => {
          closeHome();
          closeDrawer();
          if (terminalOpen) onLeaveTerminal();
          window.dispatchEvent(new Event(OPEN_COMMAND_PALETTE_EVENT));
        }}
      >
        <Search size={20} aria-hidden="true" />
        <span>Search</span>
      </button>
      <div className="phone-tab phone-tab-account">
        <UserMenu label="You" />
      </div>
    </nav>
  );
}
