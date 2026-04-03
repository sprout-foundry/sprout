import React, { useState, useEffect, useCallback, useRef, useLayoutEffect, type RefObject } from 'react';
import { Copy, ArrowDownToLine } from 'lucide-react';
import { copyToClipboard } from '../utils/clipboard';

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
  const menuRef = useRef<HTMLDivElement>(null);
  const attachedRef = useRef(false);
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

  const [copiedLabel, setCopiedLabel] = useState<string | null>(null);

  const showCopied = useCallback((label: string) => {
    setCopiedLabel(label);
    timersRef.current.push(window.setTimeout(() => setCopiedLabel(null), 1200));
  }, [clearTimers]);

  // ── Close helpers ─────────────────────────────────────────

  const close = useCallback(() => {
    setMenu((prev) => ({ ...prev, visible: false }));
  }, []);

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

  const handleCopyMessage = useCallback(async () => {
    if (!menu.messageContent) return;
    await copyToClipboard(menu.messageContent);
    showCopied('Copied!');
    timersRef.current.push(window.setTimeout(() => close(), 800));
  }, [menu.messageContent, close, showCopied]);

  const handleCopyCodeBlock = useCallback(async () => {
    if (!menu.codeBlockText) return;
    await copyToClipboard(menu.codeBlockText);
    showCopied('Copied!');
    timersRef.current.push(window.setTimeout(() => close(), 800));
  }, [menu.codeBlockText, close, showCopied]);

  const handleInsertAtCursor = useCallback(() => {
    if (!menu.messageContent) return;
    onInsertAtCursor(menu.messageContent);
    close();
  }, [menu.messageContent, onInsertAtCursor, close]);

  if (!menu.visible) return null;

  return (
    <div
      ref={menuRef}
      className="chat-msg-context-menu"
      style={{ left: menu.x, top: menu.y }}
    >
      <button
        className="chat-msg-context-menu-item"
        onClick={handleCopyMessage}
        type="button"
        disabled={!!copiedLabel}
      >
        <Copy size={13} />
        <span className="menu-item-label">{copiedLabel || 'Copy message'}</span>
      </button>

      {menu.codeBlockText && (
        <button
          className="chat-msg-context-menu-item"
          onClick={handleCopyCodeBlock}
          type="button"
          disabled={!!copiedLabel}
        >
          <Copy size={13} />
          <span className="menu-item-label">{copiedLabel || 'Copy code block'}</span>
        </button>
      )}

      <div className="chat-msg-context-menu-divider" />

      <button
        className="chat-msg-context-menu-item"
        onClick={handleInsertAtCursor}
        type="button"
      >
        <ArrowDownToLine size={13} />
        <span className="menu-item-label">Insert at cursor</span>
      </button>
    </div>
  );
};

export default ChatMessageContextMenu;
