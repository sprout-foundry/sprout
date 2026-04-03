import React, { useState, useEffect, useCallback, useRef, type RefObject } from 'react';
import { Copy, ArrowDownToLine } from 'lucide-react';
import { copyToClipboard } from '../utils/clipboard';
import ContextMenu from './ContextMenu';

interface ChatMessageMenuState {
  visible: boolean;
  x: number;
  y: number;
  messageContent: string;
  codeBlockText: string | null;
}

interface ChatMessageContextMenuProps {
  containerRef: RefObject<HTMLDivElement>;
  onInsertAtCursor: (text: string) => void;
}

/**
 * Standalone context menu for chat messages.
 * Listens for `contextmenu` events on the given container ref and shows
 * a menu with Copy / Copy Code Block / Insert at Cursor actions.
 */
const ChatMessageContextMenu: React.FC<ChatMessageContextMenuProps> = ({
  containerRef,
  onInsertAtCursor,
}) => {
  const timersRef = useRef<number[]>([]);

  // Clean up all pending timers
  const clearTimers = useCallback(() => {
    timersRef.current.forEach(clearTimeout);
    timersRef.current = [];
  }, []);

  const [menu, setMenu] = useState<ChatMessageMenuState>({
    visible: false,
    x: 0,
    y: 0,
    messageContent: '',
    codeBlockText: null,
  });

  const [copiedAction, setCopiedAction] = useState<'message' | 'code' | null>(null);

  const showCopied = useCallback((action: 'message' | 'code') => {
    setCopiedAction(action);
    timersRef.current.push(window.setTimeout(() => setCopiedAction(null), 1200));
  }, []);

  // ── Close helpers ─────────────────────────────────────────

  const close = useCallback(() => {
    clearTimers();
    setMenu((prev) => ({ ...prev, visible: false }));
  }, [clearTimers]);

  // ── Find message-bubble ancestor and code block ───────────

  const resolveMenuData = useCallback((target: HTMLElement): ChatMessageMenuState | null => {
    // Walk up to find the closest .message-bubble with data-message-content
    const bubble = target.closest('[data-message-content]') as HTMLElement | null;
    if (!bubble) return null;

    const messageContent = bubble.getAttribute('data-message-content') || '';

    // Walk up from target to find a <pre> (code block)
    let codeBlockText: string | null = null;
    const pre = target.closest('pre');
    if (pre) {
      // Prefer <code> child if available (markdown code blocks render as <pre><code>)
      const code = pre.querySelector('code');
      codeBlockText = (code || pre).textContent?.trim() || null;
    }

    return { visible: true, x: 0, y: 0, messageContent, codeBlockText };
  }, []);

  // ── Context menu handler ──────────────────────────────────

  const handleContextMenu = useCallback((e: MouseEvent) => {
    const container = containerRef.current;
    if (!container || !container.contains(e.target as Node)) return;

    const data = resolveMenuData(e.target as HTMLElement);
    if (!data) return;

    e.preventDefault();

    setMenu({
      visible: true,
      x: e.clientX,
      y: e.clientY,
      messageContent: data.messageContent,
      codeBlockText: data.codeBlockText,
    });
  }, [containerRef, resolveMenuData]);

  // ── Attach / detach contextmenu listener ──────────────────

  useEffect(() => {
    document.addEventListener('contextmenu', handleContextMenu);
    return () => document.removeEventListener('contextmenu', handleContextMenu);
  }, [handleContextMenu]);

  // ── Cleanup pending timers on unmount ─────────────────────

  useEffect(() => {
    return () => { clearTimers(); };
  }, [clearTimers]);

  // ── Action handlers ───────────────────────────────────────

  const handleCopyMessage = useCallback(async () => {
    if (!menu.messageContent) return;
    await copyToClipboard(menu.messageContent);
    showCopied('message');
    timersRef.current.push(window.setTimeout(() => close(), 800));
  }, [menu.messageContent, close, showCopied]);

  const handleCopyCodeBlock = useCallback(async () => {
    if (!menu.codeBlockText) return;
    await copyToClipboard(menu.codeBlockText);
    showCopied('code');
    timersRef.current.push(window.setTimeout(() => close(), 800));
  }, [menu.codeBlockText, close, showCopied]);

  const handleInsertAtCursor = useCallback(() => {
    if (!menu.messageContent) return;
    onInsertAtCursor(menu.messageContent);
    close();
  }, [menu.messageContent, onInsertAtCursor, close]);

  return (
    <ContextMenu isOpen={menu.visible} x={menu.x} y={menu.y} onClose={close}>
      <button
        className="context-menu-item"
        onClick={handleCopyMessage}
        type="button"
        disabled={copiedAction === 'message'}
      >
        <Copy size={13} />
        <span className="menu-item-label">{copiedAction === 'message' ? 'Copied!' : 'Copy message'}</span>
      </button>

      {menu.codeBlockText && (
        <button
          className="context-menu-item"
          onClick={handleCopyCodeBlock}
          type="button"
          disabled={copiedAction === 'code'}
        >
          <Copy size={13} />
          <span className="menu-item-label">{copiedAction === 'code' ? 'Copied!' : 'Copy code block'}</span>
        </button>
      )}

      <div className="context-menu-divider" />

      <button
        className="context-menu-item"
        onClick={handleInsertAtCursor}
        type="button"
      >
        <ArrowDownToLine size={13} />
        <span className="menu-item-label">Insert at cursor</span>
      </button>
    </ContextMenu>
  );
};

export default ChatMessageContextMenu;
