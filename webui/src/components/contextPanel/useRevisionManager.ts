import { normalizeRevision, buildRevisionFileKey } from '@sprout/ui';
import { useState, useEffect, useCallback, useRef } from 'react';
import { useLog } from '../../utils/log';
import type { ChatContextPanelProps, ChatTabId, Revision, RevisionDetailFile } from './types';

export function useRevisionManager(chatProps: ChatContextPanelProps | null, chatTab: ChatTabId) {
  const log = useLog();
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [expandedRevisionIds, setExpandedRevisionIds] = useState<Set<string>>(new Set());
  const [_revisionDetailsById, setRevisionDetailsById] = useState<Record<string, Record<string, string>>>({});
  const revisionDetailsByIdRef = useRef<Record<string, Record<string, string>>>({});
  const [revisionDetailsLoading, setRevisionDetailsLoading] = useState<Record<string, boolean>>({});
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const historyLoadRequestRef = useRef(0);

  const loadRevisionHistory = useCallback(async () => {
    if (!chatProps) return;
    const requestId = ++historyLoadRequestRef.current;
    setIsLoadingHistory(true);
    setRevisionDetailsById({});
    revisionDetailsByIdRef.current = {};
    setRevisionDetailsLoading({});
    try {
      const response = await chatProps.onLoadRevisionHistory();
      if (requestId !== historyLoadRequestRef.current) return;
      const normalized = (response.revisions || []).map(normalizeRevision).sort((a, b) => {
        return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
      });
      setRevisions(normalized);
      setExpandedRevisionIds(normalized.length > 0 ? new Set([normalized[0].revision_id]) : new Set());
    } catch (error) {
      if (requestId !== historyLoadRequestRef.current) return;
      log.error(`Failed to load revision history: ${error instanceof Error ? error.message : String(error)}`, {
        title: 'Revision History Error',
      });
    } finally {
      if (requestId === historyLoadRequestRef.current) {
        setIsLoadingHistory(false);
      }
    }
  }, [chatProps, log]);

  const loadRevisionDetails = useCallback(
    async (revisionId: string) => {
      if (!chatProps || !revisionId) return;
      if (revisionDetailsByIdRef.current[revisionId]) return;
      if (revisionDetailsLoading[revisionId]) return;

      setRevisionDetailsLoading((prev) => ({ ...prev, [revisionId]: true }));

      try {
        const response = await chatProps.onLoadRevisionDetails(revisionId);
        const detailsMap: Record<string, string> = {};
        (response.revision?.files || []).forEach((file: RevisionDetailFile, index: number) => {
          detailsMap[buildRevisionFileKey(file, index)] = file.diff || '';
        });
        setRevisionDetailsById((prev) => ({ ...prev, [revisionId]: detailsMap }));
        revisionDetailsByIdRef.current[revisionId] = detailsMap;
      } catch (error) {
        log.error(`Failed to load revision details: ${error instanceof Error ? error.message : String(error)}`, {
          title: 'Revision Details Error',
        });
      } finally {
        setRevisionDetailsLoading((prev) => ({ ...prev, [revisionId]: false }));
      }
    },
    [chatProps, revisionDetailsLoading, log],
  );

  // Auto-load on tab switch
  useEffect(() => {
    if (chatTab === 'changes' && revisions.length === 0 && !isLoadingHistory) {
      loadRevisionHistory();
    }
  }, [chatTab, revisions.length, isLoadingHistory, loadRevisionHistory]);

  // Load details for expanded revisions
  useEffect(() => {
    if (expandedRevisionIds.size === 0) return;
    expandedRevisionIds.forEach((revisionId) => {
      loadRevisionDetails(revisionId);
    });
  }, [expandedRevisionIds, loadRevisionDetails]);

  // Listen for global event to open revision history
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const openHistoryPanel = () => {
      loadRevisionHistory();
    };
    window.addEventListener('sprout:open-revision-history', openHistoryPanel);
    return () => window.removeEventListener('sprout:open-revision-history', openHistoryPanel);
  }, [loadRevisionHistory]);

  return {
    revisions,
    expandedRevisionIds,
    setExpandedRevisionIds,
    revisionDetailsById: revisionDetailsByIdRef.current,
    revisionDetailsLoading,
    isLoadingHistory,
    setIsLoadingHistory,
    loadRevisionHistory,
    loadRevisionDetails,
  };
}
