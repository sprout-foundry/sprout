/**
 * Platform pages inside the layered layout's main area. The frame loads once,
 * on first open, and then stays mounted (hidden while you work) so returning
 * to it — or leaving it — is instant; the editor underneath is never
 * unloaded either.
 */

import { useEffect, useRef, useState, type ReactElement } from 'react';
import { getActiveRepoURL } from '../../services/activeRepo';
import { closeHome, syncHomePath, useHomeView } from '../../services/homeView';
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

export default function PlatformHome(): ReactElement | null {
  const { open, path } = useHomeView();
  const frameRef = useRef<HTMLIFrameElement>(null);
  const [mounted, setMounted] = useState(open);
  const initialPath = useRef(path);

  useEffect(() => {
    if (open) setMounted(true);
  }, [open]);

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
        const params = new URLSearchParams(window.location.search);
        params.set('repo', repo);
        window.location.search = params.toString();
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
    <div className={`platform-home${open ? ' open' : ''}`} aria-hidden={!open} data-testid="platform-home">
      <iframe
        ref={frameRef}
        title="Sprout Foundry"
        src={platformHref(`/?embed=1#${initialPath.current}`)}
        className="platform-home-frame"
      />
    </div>
  );
}
