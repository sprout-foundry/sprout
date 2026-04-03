import React, { useState, useEffect, useCallback, useRef, useLayoutEffect } from 'react';
import { Copy, GitBranch, RotateCcw } from 'lucide-react';
import type { ApiService } from '../services/api';
import { copyToClipboard } from '../utils/clipboard';
import './GitHistoryPanel.css';

interface GitHistoryMenuState {
  visible: boolean;
  x: number;
  y: number;
  commitHash: string;
  commitShortHash: string;
  commitMessage: string;
}

interface GitHistoryContextMenuProps {
  apiService: ApiService;
  isActing?: boolean;
}

/**
 * Standalone context menu for git history commit rows.
 * Listens for `contextmenu` events on the document and shows a menu with
 * copy / checkout / revert actions when a `.git-history-commit-row` is targeted.
 */
const GitHistoryContextMenu: React.FC<GitHistoryContextMenuProps> = ({
  apiService,
  isActing = false,
}) => {
  const menuRef = useRef<HTMLDivElement>(null);
  const attachedRef = useRef(false);
  const timersRef = useRef<number[]>([]);

  const clearTimers = useCallback(() => {
    timersRef.current.forEach(clearTimeout);
    timersRef.current = [];
  }, []);

  const [menu, setMenu] = useState<GitHistoryMenuState>({
    visible: false,
    x: 0,
    y: 0,
    commitHash: '',
    commitShortHash: '',
    commitMessage: '',
  });

  const [copiedAction, setCopiedAction] = useState<'sha' | 'message' | null>(null);
  const [actionStatus, setActionStatus] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  const showCopied = useCallback((action: 'sha' | 'message') => {
    setCopiedAction(action);
    timersRef.current.push(window.setTimeout(() => setCopiedAction(null), 1200));
  }, []);

  // ── Close helpers ─────────────────────────────────────────

  const close = useCallback(() => {
    clearTimers();
    setMenu((prev) => ({ ...prev, visible: false }));
  }, [clearTimers]);

  // ── Context menu handler ──────────────────────────────────

  const handleContextMenu = useCallback((e: MouseEvent) => {
    const target = e.target as HTMLElement;
    const row = target.closest('.git-history-commit-row') as HTMLElement | null;
    if (!row) return;

    const commitHash = row.getAttribute('data-commit-hash') || '';
    const commitShortHash = row.getAttribute('data-commit-short-hash') || '';
    const commitMessage = row.getAttribute('data-commit-message') || '';

    if (!commitHash) return;

    e.preventDefault();

    setMenu({
      visible: true,
      x: e.clientX,
      y: e.clientY,
      commitHash,
      commitShortHash,
      commitMessage,
    });

    // Reset action status when opening
    setActionStatus(null);
    setCopiedAction(null);
  }, []);

  // ── Attach / detach contextmenu listener ──────────────────

  useEffect(() => {
    document.addEventListener('contextmenu', handleContextMenu);
    return () => document.removeEventListener('contextmenu', handleContextMenu);
  }, [handleContextMenu]);

  // ── Cleanup pending timers on unmount ─────────────────────

  useEffect(() => {
    return () => { clearTimers(); };
  }, [clearTimers]);

  // ── Global close listeners (with rAF race-condition guard) ─

  useEffect(() => {
    if (!menu.visible) {
      attachedRef.current = false;
      return;
    }

    const handleClickOutside = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        close();
      }
    };

    const handleScroll = () => {
      close();
    };

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        close();
      }
    };

    const timer = requestAnimationFrame(() => {
      if (attachedRef.current) return;
      attachedRef.current = true;
      document.addEventListener('mousedown', handleClickOutside);
      window.addEventListener('scroll', handleScroll, true);
      document.addEventListener('keydown', handleKeyDown);
      window.addEventListener('blur', close);
    });

    return () => {
      cancelAnimationFrame(timer);
      if (attachedRef.current) {
        attachedRef.current = false;
        document.removeEventListener('mousedown', handleClickOutside);
        window.removeEventListener('scroll', handleScroll, true);
        document.removeEventListener('keydown', handleKeyDown);
        window.removeEventListener('blur', close);
      }
    };
  }, [menu.visible, close]);

  // ── Viewport boundary clamping ────────────────────────────

  useLayoutEffect(() => {
    if (!menu.visible || !menuRef.current) return;
    const el = menuRef.current;

    el.style.left = '';
    el.style.top = '';

    const rect = el.getBoundingClientRect();
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const pad = 8;

    if (rect.right > vw) {
      el.style.left = `${Math.max(pad, vw - rect.width - pad)}px`;
    }
    if (rect.bottom > vh) {
      el.style.top = `${Math.max(pad, vh - rect.height - pad)}px`;
    }
  }, [menu.visible, menu.x, menu.y]);

  // ── Action handlers ───────────────────────────────────────

  const handleCopySha = useCallback(async () => {
    if (!menu.commitHash) return;
    await copyToClipboard(menu.commitHash);
    showCopied('sha');
    timersRef.current.push(window.setTimeout(() => close(), 800));
  }, [menu.commitHash, close, showCopied]);

  const handleCopyMessage = useCallback(async () => {
    if (!menu.commitMessage) return;
    await copyToClipboard(menu.commitMessage);
    showCopied('message');
    timersRef.current.push(window.setTimeout(() => close(), 800));
  }, [menu.commitMessage, close, showCopied]);

  const handleCheckout = useCallback(async () => {
    if (!menu.commitHash || isLoading || isActing) return;
    const short = menu.commitShortHash || menu.commitHash.slice(0, 7);
    const confirmed = window.confirm(
      `Checkout commit ${short}?\n\nThis will put you in a detached HEAD state.`,
    );
    if (!confirmed) {
      close();
      return;
    }

    setIsLoading(true);
    try {
      await apiService.checkoutGitCommit(menu.commitHash);
      setActionStatus('Checked out!');
      timersRef.current.push(window.setTimeout(() => close(), 800));
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Checkout failed';
      setActionStatus(msg);
      timersRef.current.push(window.setTimeout(() => {
        setActionStatus(null);
      }, 2000));
    } finally {
      setIsLoading(false);
    }
  }, [menu.commitHash, menu.commitShortHash, apiService, close, isLoading, isActing]);

  const handleRevert = useCallback(async () => {
    if (!menu.commitHash || isLoading || isActing) return;
    const short = menu.commitShortHash || menu.commitHash.slice(0, 7);
    const confirmed = window.confirm(
      `Revert commit ${short}?\n\nThis will create a new commit that undoes the changes.`,
    );
    if (!confirmed) {
      close();
      return;
    }

    setIsLoading(true);
    try {
      await apiService.revertGitCommit(menu.commitHash);
      setActionStatus('Reverted!');
      timersRef.current.push(window.setTimeout(() => close(), 800));
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Revert failed';
      setActionStatus(msg);
      timersRef.current.push(window.setTimeout(() => {
        setActionStatus(null);
      }, 2000));
    } finally {
      setIsLoading(false);
    }
  }, [menu.commitHash, menu.commitShortHash, apiService, close, isLoading, isActing]);

  if (!menu.visible) return null;

  const short = menu.commitShortHash || menu.commitHash.slice(0, 7);

  return (
    <div
      ref={menuRef}
      className="git-history-context-menu"
      style={{ left: menu.x, top: menu.y }}
    >
      {actionStatus && (
        <div className="git-history-context-menu-status">{actionStatus}</div>
      )}

      <button
        className="git-history-context-menu-item"
        onClick={handleCopySha}
        type="button"
        disabled={copiedAction === 'sha'}
      >
        <Copy size={13} />
        <span className="menu-item-label">
          {copiedAction === 'sha' ? 'Copied!' : `Copy commit SHA (${short})`}
        </span>
      </button>

      <button
        className="git-history-context-menu-item"
        onClick={handleCopyMessage}
        type="button"
        disabled={copiedAction === 'message'}
      >
        <Copy size={13} />
        <span className="menu-item-label">
          {copiedAction === 'message' ? 'Copied!' : 'Copy commit message'}
        </span>
      </button>

      <div className="git-history-context-menu-divider" />

      <button
        className="git-history-context-menu-item"
        onClick={handleCheckout}
        type="button"
        disabled={isLoading || (actionStatus !== null && actionStatus !== 'Checked out!')}
      >
        <GitBranch size={13} />
        <span className="menu-item-label">
          {isLoading
            ? 'Checking out…'
            : actionStatus === 'Checked out!'
              ? '✓ Checked out'
              : `Checkout this commit (${short})`}
        </span>
      </button>

      <button
        className="git-history-context-menu-item danger"
        onClick={handleRevert}
        type="button"
        disabled={isLoading || (actionStatus !== null && actionStatus !== 'Reverted!')}
      >
        <RotateCcw size={13} />
        <span className="menu-item-label">
          {isLoading
            ? 'Reverting…'
            : actionStatus === 'Reverted!'
              ? '✓ Reverted'
              : `Revert commit (${short})`}
        </span>
      </button>
    </div>
  );
};

export default GitHistoryContextMenu;
