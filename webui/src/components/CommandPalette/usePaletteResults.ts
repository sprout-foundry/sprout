import { useMemo, useCallback } from 'react';
import { fuzzyFilter, highlightMatches } from '../../utils/fuzzyMatch';
import type { FuzzyResult } from '../../utils/fuzzyMatch';
import type { SymbolInfo } from '../../utils/symbolUtils';
import type { PaletteMode, FileResult, PaletteResult } from './types';
import { parsePrefixAndQuery } from './utils';
import { toWorkspaceRelativePath, getDirectoryName } from './utils';
import {
  VISIBLE_COMMANDS,
  MAX_FILE_RESULTS,
  MAX_SYMBOL_RESULTS,
  NAVIGABLE_KINDS,
} from './constants';

interface UsePaletteResultsOptions {
  query: string;
  mode: PaletteMode;
  allFiles: FileResult[];
  allSymbols: SymbolInfo[];
  scopePaths: Map<number, string>;
  workspaceRoot: string;
  activeBufferFileExtension?: string;
  selectedIndex: number;
}

interface UsePaletteResultsReturn {
  results: PaletteResult[];
  navigableItems: PaletteResult[];
  selectedFlatIndex: number;
  effectiveMode: PaletteMode;
  searchQuery: string;
  toNavigableIndex: (resultIndex: number) => number;
}

function usePaletteResults(options: UsePaletteResultsOptions): UsePaletteResultsReturn {
  const { query, mode, allFiles, allSymbols, scopePaths, workspaceRoot, activeBufferFileExtension, selectedIndex } = options;

  // ── Resolve effective mode and search query ────────────────────────────

  const { effectiveMode, searchQuery } = useMemo(() => {
    if (!query) return { effectiveMode: mode as PaletteMode, searchQuery: '' };
    const parsed = parsePrefixAndQuery(query);
    if (parsed.prefix) return { effectiveMode: parsed.prefix, searchQuery: parsed.query };
    return { effectiveMode: mode as PaletteMode, searchQuery: query };
  }, [query, mode]);

  // ── Build unified results ──────────────────────────────────────────────

  const results = useMemo((): PaletteResult[] => {
    const trimmed = searchQuery.trim();
    const em = effectiveMode;

    if (!trimmed) {
      // No query — show category-grouped commands or mode default
      if (em === 'files') return [];
      if (em === 'symbols') {
        const items: PaletteResult[] = [];
        for (const sym of allSymbols) {
          const scope = scopePaths.get(sym.line);
          items.push({
            kind: 'symbol',
            highlightedLabel: sym.name,
            score: 0,
            symbolLine: sym.line,
            symbolKind: sym.kind,
            scopePath: scope,
          });
        }
        return items;
      }
      if (em === 'commands') {
        const items: PaletteResult[] = [];
        let lastCat = '';
        for (const cmd of VISIBLE_COMMANDS) {
          if (cmd.category !== lastCat) {
            lastCat = cmd.category;
            items.push({ kind: 'commands-header', highlightedLabel: cmd.category, score: 0 });
          }
          items.push({
            kind: 'command',
            commandId: cmd.id,
            commandLabel: cmd.label,
            highlightedLabel: cmd.label,
            score: 0,
          });
        }
        return items;
      }
      // 'all' — show all commands as the homepage
      const items: PaletteResult[] = [];
      let lastCat = '';
      for (const cmd of VISIBLE_COMMANDS) {
        if (cmd.category !== lastCat) {
          lastCat = cmd.category;
          items.push({ kind: 'commands-header', highlightedLabel: cmd.category, score: 0 });
        }
        items.push({
          kind: 'command',
          commandId: cmd.id,
          commandLabel: cmd.label,
          highlightedLabel: cmd.label,
          score: 0,
        });
      }
      return items;
    }

    const items: PaletteResult[] = [];

    // ── Commands ─────────────────────────────────────────────────────────
    if (em === 'all' || em === 'commands') {
      const cmdResults: FuzzyResult<typeof VISIBLE_COMMANDS[number]>[] = fuzzyFilter(trimmed, VISIBLE_COMMANDS, (c) => c.label, 50);
      if (cmdResults.length > 0) {
        items.push({ kind: 'commands-header', highlightedLabel: 'Commands', score: Infinity });
        for (const r of cmdResults) {
          items.push({
            kind: 'command',
            commandId: r.item.id,
            commandLabel: r.item.label,
            highlightedLabel: highlightMatches(r.item.label, r.matches),
            score: r.score,
          });
        }
      }
    }

    // ── Files ────────────────────────────────────────────────────────────
    if ((em === 'all' || em === 'files') && allFiles.length > 0) {
      const fileResults: FuzzyResult<FileResult>[] = fuzzyFilter(
        trimmed,
        allFiles,
        (f) => toWorkspaceRelativePath(f.path, workspaceRoot),
        MAX_FILE_RESULTS,
      );
      if (fileResults.length > 0) {
        items.push({ kind: 'files-header', highlightedLabel: 'Files', score: -1 });
        for (const r of fileResults) {
          const relativePath = toWorkspaceRelativePath(r.item.path, workspaceRoot);
          const directoryPath = getDirectoryName(relativePath);
          items.push({
            kind: 'file',
            filePath: r.item.path,
            fileName: r.item.name,
            fileDirectory: directoryPath,
            highlightedLabel: highlightMatches(r.item.name, []),
            secondaryHighlightedLabel: directoryPath ? highlightMatches(directoryPath, []) : '',
            score: r.score,
          });
        }
      }
    }

    // ── Symbols ──────────────────────────────────────────────────────────
    if ((em === 'all' || em === 'symbols') && allSymbols.length > 0) {
      const symResults: FuzzyResult<SymbolInfo>[] = fuzzyFilter(trimmed, allSymbols, (s) => s.name, MAX_SYMBOL_RESULTS);
      if (symResults.length > 0) {
        items.push({ kind: 'symbols-header', highlightedLabel: `Symbols (${activeBufferFileExtension || 'current file'})`, score: -2 });
        for (const r of symResults) {
          const scope = scopePaths.get(r.item.line);
          items.push({
            kind: 'symbol',
            highlightedLabel: highlightMatches(r.item.name, r.matches),
            score: r.score,
            symbolLine: r.item.line,
            symbolKind: r.item.kind,
            scopePath: scope,
          });
        }
      }
    }

    return items;
  }, [searchQuery, effectiveMode, allFiles, allSymbols, scopePaths, workspaceRoot, activeBufferFileExtension]);

  // ── Navigable items (skip headers) ─────────────────────────────────────

  const navigableItems = useMemo(() => results.filter((r) => NAVIGABLE_KINDS.has(r.kind)), [results]);

  // ── Map selectedIndex → flat result index ──────────────────────────────

  const selectedFlatIndex = useMemo(() => {
    let navCount = 0;
    for (let i = 0; i < results.length; i++) {
      if (NAVIGABLE_KINDS.has(results[i].kind)) {
        if (navCount === selectedIndex) return i;
        navCount++;
      }
    }
    return -1;
  }, [results, selectedIndex]);

  // ── Map flat result index → navigable index ───────────────────────────

  const toNavigableIndex = useCallback(
    (resultIndex: number) => {
      let count = 0;
      for (let i = 0; i < results.length; i++) {
        if (NAVIGABLE_KINDS.has(results[i].kind)) {
          if (i === resultIndex) return count;
          count++;
        }
      }
      return 0;
    },
    [results],
  );

  return { results, navigableItems, selectedFlatIndex, effectiveMode, searchQuery, toNavigableIndex };
}

export default usePaletteResults;
