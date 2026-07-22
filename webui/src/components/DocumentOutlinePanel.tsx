import { useState, useMemo, useRef, useEffect, useCallback } from 'react';
import type { ChangeEvent } from 'react';
import './DocumentOutlinePanel.css';
import { fuzzyFilter, highlightMatches } from '../utils/fuzzyMatch';
import { extractSymbols, findSymbolScopeEnd, KIND_ICONS, CONTAINER_KINDS, type SymbolInfo } from '../utils/symbolUtils';
import { ChevronRight, ChevronDown, Search, ListTree, UnfoldVertical, FoldVertical, FileCode } from 'lucide-react';

/** Debounce delay for content-driven re-extraction (ms). The outline doesn't
 *  need to update on every keystroke — this coalesces rapid edits into a
 *  single symbol extraction pass. */
const SYMBOL_DEBOUNCE_MS = 300;

/**
 * Tree node structure for the outline
 */
interface OutlineTreeNode {
  symbol: SymbolInfo;
  children: OutlineTreeNode[];
  containerEndLine: number;
}

/**
 * Props for the DocumentOutlinePanel component
 */
interface DocumentOutlinePanelProps {
  /** The file content to extract symbols from */
  content: string;
  /** File extension for language detection (e.g., '.ts', '.go', '.py') */
  fileExtension?: string;
  /** The current cursor line (1-based) for sync highlighting */
  cursorLine: number;
  /** Callback when user clicks a symbol — navigate editor to this line */
  onNavigateToSymbol: (line: number) => void;
  /** Whether a real file is open (not chat, diff, or welcome) */
  isFileOpen: boolean;
  /** Width of the panel */
  panelWidth?: number;
  /** Whether the panel is collapsed */
  isCollapsed?: boolean;
  /** Callback to toggle panel collapse */
  onToggleCollapse?: () => void;
}

/**
 * Build a hierarchical tree from flat symbol list based on scope containment.
 * Container symbols (function, method, class, interface) can contain other symbols.
 */
function buildSymbolTree(symbols: SymbolInfo[], content: string, languageId: string | undefined): OutlineTreeNode[] {
  if (symbols.length === 0) return [];

  const lines = content.split('\n');

  // Sort symbols by line number ascending
  const sortedSymbols = [...symbols].sort((a, b) => a.line - b.line);

  // Pre-calculate container end lines
  const containerRanges: Map<number, number> = new Map();
  for (const sym of sortedSymbols) {
    if (CONTAINER_KINDS.has(sym.kind)) {
      const endLine = findSymbolScopeEnd(lines, sym.line - 1, languageId);
      containerRanges.set(sym.line, endLine);
    }
  }

  // Build tree by nesting non-container symbols into the innermost containing container
  const rootNodes: OutlineTreeNode[] = [];
  const stack: OutlineTreeNode[] = [];

  for (const sym of sortedSymbols) {
    const isContainer = CONTAINER_KINDS.has(sym.kind);
    const endLine = isContainer ? (containerRanges.get(sym.line) ?? sym.line) : -1;

    const node: OutlineTreeNode = {
      symbol: sym,
      children: [],
      containerEndLine: endLine,
    };

    // Find the innermost container that contains this symbol
    let parentNode: OutlineTreeNode | null = null;

    // Check stack from top (innermost) to bottom (outermost)
    for (let i = stack.length - 1; i >= 0; i--) {
      const container = stack[i];
      if (sym.line <= container.containerEndLine) {
        parentNode = container;
        break;
      }
    }

    if (parentNode) {
      parentNode.children.push(node);
    } else {
      rootNodes.push(node);
    }

    // Push containers onto stack, pop when we exit their scope
    if (isContainer) {
      // Remove any nodes from stack that are no longer in scope
      while (stack.length > 0 && stack[stack.length - 1].containerEndLine < sym.line) {
        stack.pop();
      }
      stack.push(node);
    }
  }

  return rootNodes;
}

/**
 * Collect all container ancestors for a given node (depth-first traversal)
 */
function collectAllAncestors(node: OutlineTreeNode, targetLine: number): OutlineTreeNode[] {
  const ancestors: OutlineTreeNode[] = [];
  const stack: OutlineTreeNode[] = [node];

  while (stack.length > 0) {
    const current = stack.pop();
    if (current && targetLine >= current.symbol.line && targetLine <= current.containerEndLine) {
      ancestors.push(current);
    }
    // Add children to stack in reverse order to maintain depth-first order
    if (current) {
      for (let i = current.children.length - 1; i >= 0; i--) {
        stack.push(current.children[i]);
      }
    }
  }

  return ancestors;
}

/**
 * Get all container ancestors for a line (outermost → innermost)
 */
function getContainerAncestors(line: number, rootNodes: OutlineTreeNode[]): OutlineTreeNode[] {
  for (const root of rootNodes) {
    if (line >= root.symbol.line && line <= root.containerEndLine) {
      // Collect all ancestors, then filter to those where line is within scope
      const allAncestors = collectAllAncestors(root, line);
      return allAncestors.filter((anc) => line >= anc.symbol.line && line <= anc.containerEndLine);
    }
  }
  return [];
}

/**
 * Main DocumentOutlinePanel component
 */
function DocumentOutlinePanel({
  content,
  fileExtension,
  cursorLine,
  onNavigateToSymbol,
  isFileOpen,
  panelWidth = 260,
  isCollapsed = false,
  onToggleCollapse,
}: DocumentOutlinePanelProps): JSX.Element {
  const [searchQuery, setSearchQuery] = useState('');
  const treeRef = useRef<HTMLDivElement>(null);
  const [expandedNodes, setExpandedNodes] = useState<Set<number>>(new Set<number>());

  // Derive language ID from file extension
  const languageId = fileExtension?.toLowerCase();

  // Debounce content so symbol extraction doesn't fire on every keystroke.
  // The outline tree doesn't need to be real-time — 300ms is imperceptible
  // to the user but dramatically reduces parsing for large files.
  const [debouncedContent, setDebouncedContent] = useState(content);
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedContent(content), SYMBOL_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [content]);

  // Extract all symbols from debounced content
  const allSymbols = useMemo(() => {
    if (!debouncedContent || !isFileOpen) return [];
    return extractSymbols(debouncedContent, languageId);
  }, [debouncedContent, languageId, isFileOpen]);

  // Build the symbol tree
  const symbolTree = useMemo(() => {
    return buildSymbolTree(allSymbols, debouncedContent, languageId);
  }, [allSymbols, debouncedContent, languageId]);

  // Filter symbols based on search query
  const filteredSymbols = useMemo(() => {
    if (!searchQuery.trim()) return allSymbols;

    const results = fuzzyFilter(searchQuery, allSymbols, (s) => s.name, 500);
    return results.map((r) => r.item);
  }, [allSymbols, searchQuery]);

  // Build filtered tree (maintaining structure but only including filtered items)
  const filteredTree = useMemo(() => {
    if (!searchQuery.trim()) return symbolTree;

    // For filtered view, we show matching symbols but keep the tree structure
    // by showing parent containers if they have any matching children
    const matchingLines = new Set(filteredSymbols.map((s) => s.line));

    // Filter tree: include nodes that match or have matching descendants
    function filterNode(node: OutlineTreeNode): OutlineTreeNode | null {
      const matches = matchingLines.has(node.symbol.line);
      const filteredChildren = node.children.map(filterNode).filter((n): n is OutlineTreeNode => n !== null);

      if (matches || filteredChildren.length > 0) {
        return { ...node, children: filteredChildren };
      }
      return null;
    }

    return symbolTree.map(filterNode).filter((n): n is OutlineTreeNode => n !== null);
  }, [symbolTree, filteredSymbols, searchQuery]);

  // Get enclosing symbols for cursor highlighting (derived from symbolTree)
  const enclosingSymbols = useMemo(() => {
    if (cursorLine < 1 || symbolTree.length === 0) return [];
    const ancestors = getContainerAncestors(cursorLine, symbolTree);
    return ancestors.map((a) => a.symbol);
  }, [cursorLine, symbolTree]);

  // Auto-expand ancestors of cursor position
  useEffect(() => {
    if (cursorLine <= 0) return;
    if (symbolTree.length === 0) return;
    const ancestors = getContainerAncestors(cursorLine, symbolTree);
    if (ancestors.length > 0) {
      setExpandedNodes((prev) => {
        const next = new Set(prev);
        for (const anc of ancestors) next.add(anc.symbol.line);
        return next;
      });
    }
  }, [cursorLine, symbolTree]);

  // Expand containers matching search
  useEffect(() => {
    if (!searchQuery || filteredSymbols.length === 0) return;
    setExpandedNodes((prev) => {
      const next = new Set(prev);
      for (const sym of filteredSymbols) {
        if (CONTAINER_KINDS.has(sym.kind)) next.add(sym.line);
      }
      return next;
    });
  }, [searchQuery, filteredSymbols]);

  // Scroll active symbol into view
  useEffect(() => {
    if (!treeRef.current || enclosingSymbols.length === 0) return;

    const activeLine = enclosingSymbols[enclosingSymbols.length - 1].line;
    const activeElement = treeRef.current.querySelector(`[data-line="${activeLine}"]`);

    if (activeElement) {
      activeElement.scrollIntoView({ behavior: 'auto', block: 'nearest' });
    }
  }, [enclosingSymbols]);

  // Handle expand/collapse all
  const handleExpandAll = useCallback(() => {
    const allContainerLines = new Set<number>();
    function collectContainers(nodes: OutlineTreeNode[]) {
      for (const node of nodes) {
        if (CONTAINER_KINDS.has(node.symbol.kind)) {
          allContainerLines.add(node.symbol.line);
        }
        collectContainers(node.children);
      }
    }
    collectContainers(symbolTree);
    setExpandedNodes(new Set(allContainerLines));
  }, [symbolTree]);

  const handleCollapseAll = useCallback(() => {
    setExpandedNodes(new Set());
  }, []);

  // Handle search input change
  const handleSearchChange = (e: ChangeEvent<HTMLInputElement>) => {
    setSearchQuery(e.target.value);
  };

  // Handle toggling expansion of a single container line (called from tree nodes)
  const handleToggleLine = useCallback((line: number) => {
    setExpandedNodes((prev) => {
      const next = new Set(prev);
      if (next.has(line)) next.delete(line);
      else next.add(line);
      return next;
    });
  }, []);

  // Determine if a node line is in the active enclosing scope
  const isLineActive = useCallback(
    (line: number): boolean => {
      return enclosingSymbols.some((s) => s.line === line);
    },
    [enclosingSymbols],
  );

  // Handle symbol selection
  const handleSymbolClick = useCallback(
    (line: number) => {
      onNavigateToSymbol(line);
    },
    [onNavigateToSymbol],
  );

  // Render empty state
  const renderEmpty = () => {
    if (!isFileOpen) {
      return (
        <div className="outline-empty">
          <FileCode size={32} className="outline-empty-icon" />
          <div className="outline-empty-text">
            No file open
            <br />
            Open a file to see its outline
          </div>
        </div>
      );
    }

    if (allSymbols.length === 0) {
      return (
        <div className="outline-empty">
          <ListTree size={32} className="outline-empty-icon" />
          <div className="outline-empty-text">
            No symbols found
            <br />
            This file has no extractable symbols
          </div>
        </div>
      );
    }

    return null;
  };

  // Panel style for width
  const panelStyle: React.CSSProperties = {
    ...(panelWidth && !isCollapsed ? { width: `${panelWidth}px` } : {}),
  };

  return (
    <div
      className={`outline-panel ${isCollapsed ? 'collapsed' : ''}`}
      style={panelStyle}
      role="region"
      aria-label="Document outline"
    >
      {/* Header */}
      <div className="outline-panel-header">
        <span className="outline-panel-title">Outline</span>
        <div className="outline-panel-header-actions">
          <button
            type="button"
            className="outline-panel-toggle"
            onClick={onToggleCollapse}
            title={isCollapsed ? 'Expand panel' : 'Collapse panel'}
            aria-label={isCollapsed ? 'Expand panel' : 'Collapse panel'}
          >
            <ChevronRight size={16} />
          </button>
        </div>
      </div>

      {/* Search input */}
      {!isCollapsed && (
        <div className="outline-panel-search">
          <div className="outline-search-wrapper">
            <Search size={14} className="outline-search-icon" />
            <input
              type="text"
              className="outline-search-input"
              placeholder="Filter symbols..."
              value={searchQuery}
              onChange={handleSearchChange}
              aria-label="Filter symbols"
            />
          </div>
        </div>
      )}

      {/* Tree view */}
      {!isCollapsed && (
        <div className="outline-panel-tree" ref={treeRef}>
          {renderEmpty() || (
            <>
              {/* Expand/Collapse All buttons when there's content */}
              {allSymbols.length > 0 && (
                <div className="outline-expand-controls">
                  <button
                    type="button"
                    className="outline-panel-toggle"
                    onClick={handleExpandAll}
                    title="Expand All"
                    style={{ width: 'auto', padding: '0 6px' }}
                  >
                    <UnfoldVertical size={14} />
                  </button>
                  <button
                    type="button"
                    className="outline-panel-toggle"
                    onClick={handleCollapseAll}
                    title="Collapse All"
                    style={{ width: 'auto', padding: '0 6px' }}
                  >
                    <FoldVertical size={14} />
                  </button>
                </div>
              )}

              {/* Render filtered or full tree */}
              {(searchQuery ? filteredTree : symbolTree).map((node, idx) => (
                <TreeNodeWithState
                  key={`${node.symbol.line}-${idx}`}
                  node={node}
                  depth={0}
                  searchQuery={searchQuery}
                  isLineActive={isLineActive}
                  onNavigateSymbol={handleSymbolClick}
                  expandedNodes={expandedNodes}
                  onToggleLine={handleToggleLine}
                />
              ))}
            </>
          )}
        </div>
      )}
    </div>
  );
}

/**
 * Tree node component with internal expansion state management
 */
interface TreeNodeWithStateProps {
  node: OutlineTreeNode;
  depth: number;
  searchQuery: string;
  isLineActive: (line: number) => boolean;
  onNavigateSymbol: (line: number) => void;
  expandedNodes: Set<number>;
  onToggleLine: (line: number) => void;
}

function TreeNodeWithState({
  node,
  depth,
  searchQuery,
  isLineActive,
  onNavigateSymbol,
  expandedNodes,
  onToggleLine,
}: TreeNodeWithStateProps) {
  const isContainer = CONTAINER_KINDS.has(node.symbol.kind);
  const isExpanded = isContainer && expandedNodes.has(node.symbol.line);
  const isActive = isLineActive(node.symbol.line);

  // Get highlighted name if there's a search query
  let highlightedName: string | undefined;
  if (searchQuery) {
    const results = fuzzyFilter(searchQuery, [node.symbol], (s) => s.name, 1);
    if (results.length > 0) {
      highlightedName = highlightMatches(node.symbol.name, results[0].matches);
    }
  }

  return (
    <>
      <div
        className={`outline-tree-node ${isActive ? 'active' : ''}`}
        onClick={() => onNavigateSymbol(node.symbol.line)}
        style={{ paddingLeft: `${8 + depth * 16}px` }}
        data-line={node.symbol.line}
      >
        {isContainer ? (
          <button
            type="button"
            className={`outline-node-chevron ${isExpanded ? 'expanded' : ''}`}
            onClick={(e) => {
              e.stopPropagation();
              onToggleLine(node.symbol.line);
            }}
            aria-expanded={isExpanded}
            aria-label={isExpanded ? 'Collapse' : 'Expand'}
            style={{ background: 'transparent', border: 'none', padding: 0 }}
          >
            {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          </button>
        ) : (
          <span style={{ width: 14 }} />
        )}

        <span className={`outline-kind-icon ${node.symbol.kind}`}>{KIND_ICONS[node.symbol.kind]}</span>

        {searchQuery && highlightedName ? (
          <span
            className="outline-node-name"
            dangerouslySetInnerHTML={{
              __html: highlightedName,
            }}
          />
        ) : (
          <span className="outline-node-name">{node.symbol.name}</span>
        )}

        <span className="outline-node-line">{node.symbol.line}</span>
      </div>

      {isExpanded && node.children.length > 0 && (
        <div className="outline-children">
          {node.children.map((child, idx) => (
            <TreeNodeWithState
              key={`${child.symbol.line}-${idx}`}
              node={child}
              depth={depth + 1}
              searchQuery={searchQuery}
              isLineActive={isLineActive}
              onNavigateSymbol={onNavigateSymbol}
              expandedNodes={expandedNodes}
              onToggleLine={onToggleLine}
            />
          ))}
        </div>
      )}
    </>
  );
}

export default DocumentOutlinePanel;
