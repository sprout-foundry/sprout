import { ContextMenu } from '@sprout/ui';
import { Check, Copy, GitBranch, RotateCcw } from 'lucide-react';
import { useState, useEffect, useCallback, useRef } from 'react';
import { copyToClipboard } from '../utils/clipboard';
import { debugLog } from '../utils/log';
import { showThemedConfirm } from './ThemedDialog';
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
  onCheckoutCommit: (commitHash: string) => Promise<{ message: string }>;
  onRevertCommit: (commitHash: string) => Promise<{ message: string }>;
  isActing?: boolean;
  /** Disable the Revert menu item (e.g. browser mode has no revert). */
  revertDisabled?: boolean;
  /** Tooltip shown on disabled-for-browser items. */
  unsupportedTooltip?: string;
}

/**
 * Standalone context menu for git history commit rows.
 * Listens for `contextmenu` events on the document and shows a menu with
 * copy / checkout / revert actions when a `.git-history-commit-row` is targeted.
 */
function GitHistoryContextMenu({
  onCheckoutCommit,
  onRevertCommit,
  isActing = false,
  revertDisabled = false,
  unsupportedTooltip,
}: GitHistoryContextMenuProps): JSX.Element {
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

  // ── Close helper ──────────────────────────────────────────

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
    return () => {
      clearTimers();
    };
  }, [clearTimers]);

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
    const confirmed = await showThemedConfirm(
      `Checkout commit ${short}?\n\nThis will put you in a detached HEAD state.`,
      { type: 'warning' },
    );
    if (!confirmed) {
      close();
      return;
    }

    setIsLoading(true);
    try {
      await onCheckoutCommit(menu.commitHash);
      setActionStatus('Checked out!');
      timersRef.current.push(window.setTimeout(() => close(), 800));
    } catch (err) {
      debugLog('Failed to checkout commit:', err);
      const msg = err instanceof Error ? err.message : 'Checkout failed';
      setActionStatus(msg);
      timersRef.current.push(
        window.setTimeout(() => {
          setActionStatus(null);
        }, 2000),
      );
    } finally {
      setIsLoading(false);
    }
  }, [menu.commitHash, menu.commitShortHash, onCheckoutCommit, close, isLoading, isActing]);

  const handleRevert = useCallback(async () => {
    if (!menu.commitHash || isLoading || isActing) return;
    const short = menu.commitShortHash || menu.commitHash.slice(0, 7);
    const confirmed = await showThemedConfirm(
      `Revert commit ${short}?\n\nThis will create a new commit that undoes the changes.`,
      { type: 'warning' },
    );
    if (!confirmed) {
      close();
      return;
    }

    setIsLoading(true);
    try {
      await onRevertCommit(menu.commitHash);
      setActionStatus('Reverted!');
      timersRef.current.push(window.setTimeout(() => close(), 800));
    } catch (err) {
      debugLog('Failed to revert commit:', err);
      const msg = err instanceof Error ? err.message : 'Revert failed';
      setActionStatus(msg);
      timersRef.current.push(
        window.setTimeout(() => {
          setActionStatus(null);
        }, 2000),
      );
    } finally {
      setIsLoading(false);
    }
  }, [menu.commitHash, menu.commitShortHash, onRevertCommit, close, isLoading, isActing]);

  const short = menu.commitShortHash || menu.commitHash.slice(0, 7);

  return (
    <ContextMenu isOpen={menu.visible} x={menu.x} y={menu.y} onClose={close} className="git-history-context-menu">
      {actionStatus && <div className="git-history-context-menu-status">{actionStatus}</div>}

      <button className="context-menu-item" onClick={handleCopySha} type="button" disabled={copiedAction === 'sha'}>
        <Copy size={13} />
        <span className="menu-item-label">{copiedAction === 'sha' ? 'Copied!' : `Copy commit SHA (${short})`}</span>
      </button>

      <button
        className="context-menu-item"
        onClick={handleCopyMessage}
        type="button"
        disabled={copiedAction === 'message'}
      >
        <Copy size={13} />
        <span className="menu-item-label">{copiedAction === 'message' ? 'Copied!' : 'Copy commit message'}</span>
      </button>

      <div className="context-menu-divider" />

      <button
        className="context-menu-item"
        onClick={handleCheckout}
        type="button"
        disabled={isLoading || (actionStatus !== null && actionStatus !== 'Checked out!')}
      >
        <GitBranch size={13} />
        <span className="menu-item-label">
          {isLoading ? (
            'Checking out…'
          ) : actionStatus === 'Checked out!' ? (
            <>
              <Check size={13} /> Checked out
            </>
          ) : (
            `Checkout this commit (${short})`
          )}
        </span>
      </button>

      <button
        className="context-menu-item danger"
        onClick={handleRevert}
        type="button"
        disabled={isLoading || (actionStatus !== null && actionStatus !== 'Reverted!') || revertDisabled}
        title={revertDisabled ? unsupportedTooltip : undefined}
      >
        <RotateCcw size={13} />
        <span className="menu-item-label">
          {isLoading ? (
            'Reverting…'
          ) : actionStatus === 'Reverted!' ? (
            <>
              <Check size={13} /> Reverted
            </>
          ) : (
            `Revert commit (${short})`
          )}
        </span>
      </button>
    </ContextMenu>
  );
}

export default GitHistoryContextMenu;
