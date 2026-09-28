/**
 * Platform pages inside the layered layout's main area. The frame loads once,
 * on first open, and then stays mounted (hidden while you work) so returning
 * to it — or leaving it — is instant; the editor underneath is never
 * unloaded either.
 */

import { Menu } from 'lucide-react';
import { useEffect, useLayoutEffect, useRef, useState, type ReactElement } from 'react';
import { getActiveRepoURL } from '../../services/activeRepo';
import { closeHome, searchForRepo, syncHomePath, useHomeView } from '../../services/homeView';
import { platformHref } from '../../utils/platformUrl';

type EmbedMessage = { type: 'sprout:open-editor'; href: string } | { type: 'sprout:platform-route'; path: string };

function sameRepo(a: string | null, b: string | null): boolean {
  const norm = (u: string | null) =>
    (u ?? '')
      .trim()
      .replace(/\.git$/, '')
      .replace(/\/+$/, '')
      .toLowerCase();
  return norm(a) === norm(b);
}

interface PlatformHomeProps {
  /** Phones: Home covers the editor's own menu button, so it brings one. */
  isMobile?: boolean;
  onOpenMenu?: () => void;
}

export default function PlatformHome({ isMobile, onOpenMenu }: PlatformHomeProps): ReactElement | null {
  const { open, path } = useHomeView();
  const frameRef = useRef<HTMLIFrameElement>(null);
  const [mounted, setMounted] = useState(open);
  const initialPath = useRef(path);

  useEffect(() => {
    if (open) setMounted(true);
  }, [open]);

  // The editor's top bar (search, credits) stays visible above Home on
  // wider screens; phones get Home's own bar instead.
  const [top, setTop] = useState(0);
  useLayoutEffect(() => {
    if (!open || isMobile) {
      setTop(0);
      return;
    }
    const bar = document.querySelector<HTMLElement>('.app > main .header-bar');
    setTop(bar?.offsetHeight ?? 0);
  }, [open, isMobile]);

  // Route changes after the first load move the embedded app in place
  // (a hash change) instead of reloading the frame.
  useEffect(() => {
    const win = frameRef.current?.contentWindow;
    if (!mounted || !win) return;
    try {
      if (win.location.hash.replace(/^#/, '') !== path) win.location.hash = path;
    } catch {
      // Cross-origin frame (misconfigured platform URL): leave it be.
    }
  }, [mounted, path]);

  useEffect(() => {
    const onMessage = (e: MessageEvent<EmbedMessage>) => {
      if (e.source !== frameRef.current?.contentWindow || !e.data || typeof e.data !== 'object') return;
      if (e.data.type === 'sprout:platform-route') {
        syncHomePath(e.data.path);
        return;
      }
      if (e.data.type === 'sprout:open-editor') {
        const target = new URL(e.data.href, window.location.origin);
        const repo = target.searchParams.get('repo');
        if (!repo || sameRepo(repo, getActiveRepoURL())) {
          closeHome();
          return;
        }
        // Another repository: the editor reopens on it (clone or restore).
        window.location.search = searchForRepo(repo);
      }
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, []);

  useEffect(() => {
    document.documentElement.classList.toggle('home-open', open);
    return () => document.documentElement.classList.remove('home-open');
  }, [open]);

  if (!mounted) return null;
  return (
    <div
      className={`platform-home${open ? ' open' : ''}`}
      style={{ top }}
      aria-hidden={!open}
      data-testid="platform-home"
    >
      {isMobile && (
        <div className="platform-home-mobile-bar">
          <button type="button" className="project-nav-back" onClick={onOpenMenu} aria-label="Open navigation">
            <Menu size={18} />
          </button>
          <span>Home</span>
        </div>
      )}
      <iframe
        ref={frameRef}
        title="Sprout Foundry"
        src={platformHref(`/?embed=1#${initialPath.current}`)}
        className="platform-home-frame"
      />
    </div>
  );
}
