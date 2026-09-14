import { Clock, Download, History, Loader2, Search, X } from 'lucide-react';
import React, { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { clientFetch } from '../../services/clientSession';
import { supportsExport } from '../../config/mode';
import { getSessions, searchSessions } from '../../services/api/sessionApi';
import type { SessionEntry, SessionSearchResult } from '../../services/api/types/session';
import { showThemedConfirm } from '../ThemedDialog';
import { useLog } from '../../utils/log';
import './ChatHistorySwitcher.css';

const SEARCH_DEBOUNCE_MS = 250;
const LIST_LIMIT = 30;

function ageLabel(iso: string): string {
  const then = Date.parse(iso);
  if (!Number.isFinite(then)) return '';
  const mins = Math.round((Date.now() - then) / 60000);
  if (mins < 1) return 'now';
  if (mins < 60) return `${mins}m`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.round(hours / 24);
  if (days < 30) return `${days}d`;
  const months = Math.round(days / 30);
  return `${months}mo`;
}

/**
 * Chat-header history switcher (SP-139 Phase 3). A permanent header row
 * control that opens a popover for browsing/restoring past conversations
 * in this workspace — the relocated Sessions tab.
 *
 * Performance contract: closed popover renders ONE button; the list and
 * search fetch only on open/typing (debounced). No polling, no store
 * slices — component-local state only.
 *
 * Restore is chat-scoped: the popover passes its owning chat's id to
 * /api/sessions/restore so a background pane restores into itself, not
 * into whatever chat happens to be focused.
 */
interface ChatHistorySwitcherProps {
  chatId?: string;
  onRestoreSession?: (sessionId: string, chatId?: string) => void | Promise<void>;
}

function ChatHistorySwitcherInner({ chatId, onRestoreSession }: ChatHistorySwitcherProps) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<DOMRect | null>(null);
  const [query, setQuery] = useState('');
  const [sessions, setSessions] = useState<SessionEntry[] | null>(null);
  const [results, setResults] = useState<SessionSearchResult[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [restoring, setRestoring] = useState<string | null>(null);
  const [exporting, setExporting] = useState(false);
  const log = useLog();
  const btnRef = useRef<HTMLButtonElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const searchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const loadList = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await getSessions(clientFetch);
      setSessions(resp.sessions ?? []);
    } catch (err) {
      log.error(`Failed to list sessions: ${err instanceof Error ? err.message : String(err)}`, {
        title: 'History',
      });
      setSessions([]);
    } finally {
      setLoading(false);
    }
  }, [log]);

  const openPopover = useCallback(() => {
    setAnchor(btnRef.current?.getBoundingClientRect() ?? null);
    setOpen(true);
    void loadList();
  }, [loadList]);

  const close = useCallback(() => {
    setOpen(false);
    setQuery('');
    setResults(null);
    if (searchTimerRef.current) clearTimeout(searchTimerRef.current);
  }, []);

  // Debounced search — empty query clears back to the recent list.
  useEffect(() => {
    if (!open) return;
    const q = query.trim();
    if (!q) {
      setResults(null);
      return;
    }
    if (searchTimerRef.current) clearTimeout(searchTimerRef.current);
    searchTimerRef.current = setTimeout(() => {
      setLoading(true);
      searchSessions(clientFetch, q, { limit: LIST_LIMIT })
        .then((resp) => setResults(resp.results ?? []))
        .catch((err) => {
          log.error(`Session search failed: ${err instanceof Error ? err.message : String(err)}`, {
            title: 'History',
          });
          setResults([]);
        })
        .finally(() => setLoading(false));
    }, SEARCH_DEBOUNCE_MS);
    return () => {
      if (searchTimerRef.current) clearTimeout(searchTimerRef.current);
    };
  }, [query, open, log]);

  // Dismiss on outside click / Escape.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      const target = e.target as Node;
      if (popRef.current?.contains(target) || btnRef.current?.contains(target)) return;
      close();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close();
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open, close]);

  const onRestoreRef = useRef(onRestoreSession);
  onRestoreRef.current = onRestoreSession;

  const handleRestore = useCallback(
    async (sessionId: string, name: string) => {
      const label = name && name !== sessionId ? `"${name}"` : sessionId;
      const ok = await showThemedConfirm(
        `Restore conversation ${label} into this chat?\n\nThis replaces the current conversation. The replaced conversation stays available in history.`,
        { title: 'Restore conversation', confirmLabel: 'Restore', cancelLabel: 'Cancel', type: 'warning' },
      );
      if (!ok) return;
      setRestoring(sessionId);
      try {
        await onRestoreRef.current?.(sessionId, chatId);
        close();
      } catch {
        /* error toast owned by the caller */
      } finally {
        setRestoring(null);
      }
    },
    [chatId, close],
  );

  const handleExportAll = useCallback(async () => {
    if (exporting) return;
    setExporting(true);
    try {
      const resp = await getSessions(clientFetch, 'current');
      const targets = (resp.sessions ?? []).filter((s) => s.message_count > 0);
      for (const s of targets) {
        const url = `/api/sessions/${encodeURIComponent(s.session_id)}/export?format=markdown&include_tool_calls=false&include_cost=true`;
        try {
          const head = await fetch(url, { method: 'HEAD' });
          if (!head.ok) continue;
        } catch {
          continue;
        }
        const a = document.createElement('a');
        a.href = url;
        a.download = '';
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        await new Promise((r) => setTimeout(r, 300));
      }
    } finally {
      setExporting(false);
    }
  }, [exporting]);

  const rows = useMemo(() => {
    if (results) {
      return results.map((r) => ({
        id: r.session_id,
        name: r.name,
        preview: r.excerpt,
        age: ageLabel(r.last_updated),
        messages: undefined as number | undefined,
      }));
    }
    return (sessions ?? [])
      .slice()
      .sort((a, b) => Date.parse(b.last_updated) - Date.parse(a.last_updated))
      .slice(0, LIST_LIMIT)
      .map((s) => ({
        id: s.session_id,
        name: s.name,
        preview: `${s.message_count} messages`,
        age: ageLabel(s.last_updated),
        messages: s.message_count,
      }));
  }, [results, sessions]);

  const popover = open
    ? createPortal(
        <div
          ref={popRef}
          className="chs-popover"
          style={
            anchor
              ? {
                  position: 'fixed',
                  top: anchor.bottom + 6,
                  left: Math.max(8, anchor.left),
                  maxHeight: Math.min(420, window.innerHeight - anchor.bottom - 24),
                }
              : undefined
          }
          data-testid="chs-popover"
        >
          <div className="chs-search">
            <Search size={13} className="chs-search-icon" />
            <input
              type="text"
              className="chs-search-input"
              placeholder="Search conversations…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              autoFocus
              aria-label="Search conversations"
              data-testid="chs-search-input"
            />
            {query && (
              <button type="button" className="chs-search-clear" onClick={() => setQuery('')} aria-label="Clear">
                <X size={12} />
              </button>
            )}
          </div>
          <div className="chs-list">
            {loading ? (
              <div className="chs-loading">
                <Loader2 size={14} className="chs-spinner" />
                <span>{results ? 'Searching…' : 'Loading…'}</span>
              </div>
            ) : rows.length === 0 ? (
              <div className="chs-empty">{query ? 'No matching conversations' : 'No saved conversations yet'}</div>
            ) : (
              rows.map((r) => (
                <button
                  key={r.id}
                  type="button"
                  className={`chs-row ${restoring === r.id ? 'restoring' : ''}`}
                  onClick={() => void handleRestore(r.id, r.name)}
                  disabled={restoring !== null}
                  title={`Restore ${r.name || r.id}`}
                  data-testid="chs-row"
                >
                  {restoring === r.id ? (
                    <Loader2 size={13} className="chs-spinner" />
                  ) : (
                    <Clock size={13} className="chs-row-icon" />
                  )}
                  <span className="chs-row-body">
                    <span className="chs-row-name">{r.name || r.id}</span>
                    <span className="chs-row-preview">{r.preview}</span>
                  </span>
                  <span className="chs-row-age">{r.age}</span>
                </button>
              ))
            )}
          </div>
          <div className="chs-footer">
            {/* Cloud mode: export endpoints are not deployed — hide rather
             * than 404 (same gating as ChatView's per-chat Export button). */}
            {supportsExport && (
              <button
                type="button"
                className="chs-footer-btn"
                onClick={() => void handleExportAll()}
                disabled={exporting}
                title="Export every saved conversation as markdown"
                data-testid="chs-export-all"
              >
                {exporting ? <Loader2 size={12} className="chs-spinner" /> : <Download size={12} />}
                {exporting ? 'Exporting…' : 'Export all'}
              </button>
            )}
          </div>
        </div>,
        document.body,
      )
    : null;

  if (!onRestoreSession) return null;

  return (
    <div className="chat-history-switcher">
      <button
        ref={btnRef}
        type="button"
        className="chs-trigger"
        onClick={() => (open ? close() : openPopover())}
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Conversation history"
        data-testid="chs-trigger"
      >
        <History size={13} />
        <span>History</span>
      </button>
      {popover}
    </div>
  );
}

export const ChatHistorySwitcher = memo(ChatHistorySwitcherInner);
