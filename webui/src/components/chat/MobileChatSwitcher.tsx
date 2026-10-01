import { Check, ChevronDown, Plus } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import './MobileChatSwitcher.css';

export interface MobileChatSwitcherProps {
  chats: Array<{ id: string; name?: string; active_query?: boolean }>;
  activeChatId: string | null;
  onSelect: (id: string) => void;
  onCreate?: () => void;
}

/**
 * Chat picker for the phone layout, which has no tab strip: the current
 * chat's name opens a list of the other chats and "New chat".
 */
export function MobileChatSwitcher({ chats, activeChatId, onSelect, onCreate }: MobileChatSwitcherProps) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const active = chats.find((c) => c.id === activeChatId);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', onDown);
    return () => document.removeEventListener('pointerdown', onDown);
  }, [open]);

  return (
    <div className="mobile-chat-switcher" ref={rootRef}>
      <button
        type="button"
        className="mobile-chat-switcher-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="mobile-chat-switcher-name">{active?.name || 'Chat'}</span>
        <ChevronDown size={14} aria-hidden="true" />
      </button>
      {open && (
        <div className="mobile-chat-switcher-menu" role="listbox" aria-label="Chats">
          {chats.map((chat) => (
            <button
              key={chat.id}
              type="button"
              role="option"
              aria-selected={chat.id === activeChatId}
              className="mobile-chat-switcher-item"
              onClick={() => {
                setOpen(false);
                if (chat.id !== activeChatId) onSelect(chat.id);
              }}
            >
              <span className="mobile-chat-switcher-item-name">{chat.name || 'Chat'}</span>
              {chat.active_query && <span className="mobile-chat-switcher-running" aria-label="Working" />}
              {chat.id === activeChatId && <Check size={14} aria-hidden="true" />}
            </button>
          ))}
          {onCreate && (
            <button
              type="button"
              className="mobile-chat-switcher-item mobile-chat-switcher-new"
              onClick={() => {
                setOpen(false);
                onCreate();
              }}
            >
              <Plus size={14} aria-hidden="true" />
              <span>New chat</span>
            </button>
          )}
        </div>
      )}
    </div>
  );
}
