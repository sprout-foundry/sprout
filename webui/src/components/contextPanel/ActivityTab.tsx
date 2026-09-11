import { ChevronDown, ChevronRight, Bot, Wrench } from 'lucide-react';
import { useMemo } from 'react';
import { ToolCard } from './ToolCard';
import { SubagentsTab } from './SubagentsTab';
import { isSubagentTool } from './helpers';
import type { ContextSubagentRun, SubagentResourceCounts, ToolExecution } from './types';

interface ActivityTabProps {
  toolExecutions: ToolExecution[];
  subagentRuns: ContextSubagentRun[];
  resourceCounts: SubagentResourceCounts;
  groupedByQuery: Map<number, ToolExecution[]>;
  maxQueryId: number;
  expandedQueries: Set<number>;
  expandedTools: Set<string>;
  expandedSubagents: Set<string>;
  activeToolId: string | null;
  toolRefs: React.MutableRefObject<Record<string, HTMLElement | null>>;
  toggleQueryGroup: (queryId: number) => void;
  toggleToolExpansion: (toolId: string) => void;
  toggleSubagentExpansion: (toolId: string) => void;
  setActiveToolId: (v: string | null) => void;
  setExpandedTools: React.Dispatch<React.SetStateAction<Set<string>>>;
  setExpandedQueries: React.Dispatch<React.SetStateAction<Set<number>>>;
}

/**
 * Unified "what did the agent do" view.
 *
 * Delegated work (run_subagent / run_parallel_subagents) renders as rich
 * SubagentCards at the top — live log, task groups, cancel, result
 * preview. Plain tool calls render below as turn-grouped collapsible
 * rows, excluding subagent tools to avoid duplication.
 *
 * This replaces the former separate Tools and Subagents tabs, which were
 * filtered views of the same toolExecutions array (useSubagentRuns starts
 * from toolExecutions.filter(isSubagentTool)) and cross-linked to each
 * other.
 */
export function ActivityTab({
  toolExecutions,
  subagentRuns,
  resourceCounts,
  groupedByQuery,
  maxQueryId,
  expandedQueries,
  expandedTools,
  expandedSubagents,
  activeToolId,
  toolRefs,
  toggleQueryGroup,
  toggleToolExpansion,
  toggleSubagentExpansion,
  setActiveToolId,
  setExpandedTools,
  setExpandedQueries,
}: ActivityTabProps) {
  // Subagent executions are rendered by SubagentsTab's cards; showing them
  // again in the tool rows would duplicate every delegation event.
  const plainToolExecutions = useMemo(() => toolExecutions.filter((t) => !isSubagentTool(t)), [toolExecutions]);

  const plainGroupedByQuery = useMemo(() => {
    const groups = new Map<number, ToolExecution[]>();
    for (const tool of plainToolExecutions) {
      const qid = tool.queryId ?? 0;
      if (!groups.has(qid)) groups.set(qid, []);
      const bucket = groups.get(qid);
      if (bucket) bucket.push(tool);
    }
    return groups;
  }, [plainToolExecutions]);

  const hasSubagents = subagentRuns.length > 0;
  const hasTools = plainToolExecutions.length > 0;

  if (!hasSubagents && !hasTools) {
    return (
      <div className="context-panel-tools-list" data-testid="context-panel-activity">
        <div className="context-panel-empty">Tool calls and delegated work will appear here.</div>
      </div>
    );
  }

  return (
    <div className="context-panel-tools-list" data-testid="context-panel-activity">
      {hasSubagents && (
        <>
          <div className="activity-section-label">
            <Bot size={12} /> Delegated work
          </div>
          <SubagentsTab
            subagentRuns={subagentRuns}
            resourceCounts={resourceCounts}
            expandedSubagents={expandedSubagents}
            activeToolId={activeToolId}
            toolRefs={toolRefs}
            toggleSubagentExpansion={toggleSubagentExpansion}
          />
        </>
      )}
      {hasTools && (
        <>
          <div className="activity-section-label">
            <Wrench size={12} /> Tool calls
          </div>
          {Array.from(plainGroupedByQuery.entries()).map(([queryId, tools]) => {
            const isCurrentTurn = queryId === maxQueryId;
            // expandedQueries tracks user overrides from the default behavior.
            // Default: current turn expanded, past turns collapsed.
            const isInSet = expandedQueries.has(queryId);
            const isExpanded = isCurrentTurn ? !isInSet : isInSet;
            const groupLabel = isCurrentTurn ? 'Current turn' : queryId === 0 ? 'Earlier' : `Turn ${queryId}`;
            return (
              <div key={queryId} className={`tool-query-group${isCurrentTurn ? ' tool-query-group--current' : ''}`}>
                <div className="tool-query-header" onClick={() => toggleQueryGroup(queryId)}>
                  <span className="tool-query-chevron">
                    {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                  </span>
                  <span className="tool-query-label">{groupLabel}</span>
                  <span className="tool-query-count">
                    {tools.length} {tools.length === 1 ? 'tool' : 'tools'}
                  </span>
                </div>
                {isExpanded && (
                  <div className="tool-query-tools">
                    {tools.map((tool) => (
                      <ToolCard
                        key={tool.id}
                        tool={tool}
                        expandedTools={expandedTools}
                        activeToolId={activeToolId}
                        toolRef={toolRefs}
                        onToggleExpansion={toggleToolExpansion}
                      />
                    ))}
                  </div>
                )}
              </div>
            );
          })}
        </>
      )}
    </div>
  );
}
