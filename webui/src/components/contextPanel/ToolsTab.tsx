import { ChevronDown, ChevronRight } from 'lucide-react';
import { ToolCard } from './ToolCard';
import type { ToolExecution } from './types';

interface ToolsTabProps {
  toolExecutions: ToolExecution[];
  groupedByQuery: Map<number, ToolExecution[]>;
  maxQueryId: number;
  expandedQueries: Set<number>;
  expandedTools: Set<string>;
  activeToolId: string | null;
  toolRefs: React.MutableRefObject<Record<string, HTMLDivElement | null>>;
  toggleQueryGroup: (queryId: number) => void;
  toggleToolExpansion: (toolId: string) => void;
}

export function ToolsTab({
  toolExecutions,
  groupedByQuery,
  maxQueryId,
  expandedQueries,
  expandedTools,
  activeToolId,
  toolRefs,
  toggleQueryGroup,
  toggleToolExpansion,
}: ToolsTabProps) {
  return (
    <div className="context-panel-tools-list" data-testid="context-panel-tools">
      {toolExecutions.length === 0 ? (
        <div className="context-panel-empty">Tool calls will appear here.</div>
      ) : (
        Array.from(groupedByQuery.entries()).map(([queryId, tools]) => {
          const isCurrentTurn = queryId === maxQueryId;
          const isExpanded = isCurrentTurn || expandedQueries.has(queryId);
          const groupLabel = isCurrentTurn ? 'Current turn' : queryId === 0 ? 'Earlier' : `Turn ${queryId}`;
          return (
            <div key={queryId} className="tool-query-group">
              <div className="tool-query-header" onClick={() => toggleQueryGroup(queryId)}>
                <span className="tool-query-label">{groupLabel}</span>
                <span className="tool-query-count">
                  {tools.length} {tools.length === 1 ? 'tool' : 'tools'}
                </span>
                {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
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
        })
      )}
    </div>
  );
}
