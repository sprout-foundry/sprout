/**
 * Custom search panel extension for CodeMirror.
 *
 * Provides a modern-styled search panel with toggle buttons for case sensitivity,
 * whole-word matching, and regex mode, styled similarly to VS Code's search toggles.
 */

import { EditorView, type Panel, type ViewUpdate } from '@codemirror/view';
import { search, SearchQuery, setSearchQuery, getSearchQuery, findNext, findPrevious, selectMatches, replaceNext, replaceAll, closeSearchPanel } from '@codemirror/search';

import './searchPanel.css';

/**
 * Creates a custom search panel with styled toggle buttons.
 *
 * Features:
 * - Search input with "Find" placeholder
 * - Toggle buttons for Case Sensitive (Aa), Whole Word (Wb), Regex (.*)
 * - Replace input (hidden in read-only mode)
 * - Previous/Next/Select All buttons
 * - Replace/Replace All buttons
 * - Close button (×)
 */
function createCustomSearchPanel(view: EditorView, onSave?: () => void): Panel {
  let query = getSearchQuery(view.state) || new SearchQuery({ search: '', replace: '' });

  // Search input field
  const searchField = document.createElement('input');
  searchField.type = 'text';
  searchField.className = 'cm-textfield';
  searchField.name = 'search';
  searchField.placeholder = 'Find';
  searchField.setAttribute('main-field', 'true');
  searchField.value = query.search;

  // Replace input field
  const replaceField = document.createElement('input');
  replaceField.type = 'text';
  replaceField.className = 'cm-textfield';
  replaceField.name = 'replace';
  replaceField.placeholder = 'Replace';
  replaceField.value = query.replace;

  // Toggle buttons with icons
  const caseToggle = document.createElement('button');
  caseToggle.type = 'button';
  caseToggle.className = 'cm-search-toggle' + (query.caseSensitive ? ' cm-search-toggle-active' : '');
  caseToggle.title = 'Match Case (Alt+C)';
  caseToggle.setAttribute('aria-label', 'Match case');
  caseToggle.setAttribute('aria-pressed', query.caseSensitive ? 'true' : 'false');
  caseToggle.textContent = 'Aa';

  const wordToggle = document.createElement('button');
  wordToggle.type = 'button';
  wordToggle.className = 'cm-search-toggle' + (query.wholeWord ? ' cm-search-toggle-active' : '');
  wordToggle.title = 'Match Whole Word (Alt+W)';
  wordToggle.setAttribute('aria-label', 'Match whole word');
  wordToggle.setAttribute('aria-pressed', query.wholeWord ? 'true' : 'false');
  wordToggle.textContent = 'Wb';

  const regexToggle = document.createElement('button');
  regexToggle.type = 'button';
  regexToggle.className = 'cm-search-toggle' + (query.regexp ? ' cm-search-toggle-active' : '');
  regexToggle.title = 'Use Regular Expression (Alt+R)';
  regexToggle.setAttribute('aria-label', 'Use regular expression');
  regexToggle.setAttribute('aria-pressed', query.regexp ? 'true' : 'false');
  regexToggle.textContent = '.*';

  // Container for toggle buttons
  const togglesContainer = document.createElement('div');
  togglesContainer.className = 'cm-search-toggles';
  togglesContainer.appendChild(caseToggle);
  togglesContainer.appendChild(wordToggle);
  togglesContainer.appendChild(regexToggle);

  // Helper to create buttons with text
  function createButton(name: string, title: string, text: string): HTMLButtonElement {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'cm-button';
    btn.name = name;
    btn.title = title;
    btn.textContent = text;
    return btn;
  }

  const prevButton = createButton('prev', 'Previous Match (Shift+Enter)', 'Prev');
  const nextButton = createButton('next', 'Next Match (Enter)', 'Next');
  const selectButton = createButton('select', 'Select All Matches (Alt+Enter)', 'All');
  const replaceButton = createButton('replace', 'Replace (Ctrl+Enter)', 'Replace');
  const replaceAllButton = createButton('replaceAll', 'Replace All', 'All');

  const closeButton = createButton('close', 'Close (Escape)', '×');
  closeButton.classList.add('cm-search-close');
  closeButton.setAttribute('aria-label', 'Close search panel');

  // Commit the search query when inputs change
  function commit() {
    const newQuery = new SearchQuery({
      search: searchField.value,
      caseSensitive: caseToggle.classList.contains('cm-search-toggle-active'),
      regexp: regexToggle.classList.contains('cm-search-toggle-active'),
      wholeWord: wordToggle.classList.contains('cm-search-toggle-active'),
      replace: replaceField.value,
    });

    // Show validation feedback for invalid regex
    if (!newQuery.valid) {
      searchField.classList.add('cm-search-invalid');
      return; // Don't dispatch invalid queries
    }
    searchField.classList.remove('cm-search-invalid');

    if (!newQuery.eq(query)) {
      query = newQuery;
      view.dispatch({ effects: setSearchQuery.of(newQuery) });
    }
  }

  // Toggle button click handlers
  caseToggle.onclick = () => {
    caseToggle.classList.toggle('cm-search-toggle-active');
    const isActive = caseToggle.classList.contains('cm-search-toggle-active');
    caseToggle.setAttribute('aria-pressed', isActive ? 'true' : 'false');
    commit();
  };

  wordToggle.onclick = () => {
    wordToggle.classList.toggle('cm-search-toggle-active');
    const isActive = wordToggle.classList.contains('cm-search-toggle-active');
    wordToggle.setAttribute('aria-pressed', isActive ? 'true' : 'false');
    commit();
  };

  regexToggle.onclick = () => {
    regexToggle.classList.toggle('cm-search-toggle-active');
    const isActive = regexToggle.classList.contains('cm-search-toggle-active');
    regexToggle.setAttribute('aria-pressed', isActive ? 'true' : 'false');
    commit();
  };

  // Button click handlers
  prevButton.onclick = () => findPrevious(view);
  nextButton.onclick = () => findNext(view);
  selectButton.onclick = () => selectMatches(view);
  replaceButton.onclick = () => replaceNext(view);
  replaceAllButton.onclick = () => replaceAll(view);
  closeButton.onclick = () => closeSearchPanel(view);

  // Input change handlers
  searchField.oninput = () => commit();
  replaceField.oninput = () => commit();

  // Build the panel DOM
  const isReadOnly = view.state.readOnly;
  const dom = document.createElement('div');
  dom.className = 'cm-search';
  dom.onkeydown = (e: KeyboardEvent) => {
    // Only delegate Mod-s (save) to the editor — do NOT forward other
    // modifier-key shortcuts (Ctrl+A, Ctrl+Z, Ctrl+K etc.) because the
    // editor keymap contains bindings for these that conflict with normal
    // input field behavior (e.g. on Linux, Ctrl+K → deleteToLineEnd).
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && (e.key === 's' || e.key === 'S')) {
      e.preventDefault();
      onSave?.();
      return;
    }

    // Handle Escape
    if (e.key === 'Escape') {
      e.preventDefault();
      closeSearchPanel(view);
      return;
    }

    // Enter in search field → find next, Shift+Enter → find previous
    if (e.key === 'Enter' && e.target === searchField) {
      e.preventDefault();
      if (e.shiftKey) {
        findPrevious(view);
      } else {
        findNext(view);
      }
      return;
    }

    // Enter in replace field → replace next
    if (e.key === 'Enter' && e.target === replaceField) {
      e.preventDefault();
      replaceNext(view);
      return;
    }

    // Ctrl+Enter in search field → select all
    if (e.key === 'Enter' && e.ctrlKey && e.target === searchField) {
      e.preventDefault();
      selectMatches(view);
      return;
    }

    // Alt+C, Alt+W, Alt+R for toggles (when input is focused)
    if (e.altKey && e.target === searchField) {
      const keyLower = e.key.toLowerCase();
      if (keyLower === 'c') {
        e.preventDefault();
        caseToggle.click();
        return;
      }
      if (keyLower === 'w') {
        e.preventDefault();
        wordToggle.click();
        return;
      }
      if (keyLower === 'r') {
        e.preventDefault();
        regexToggle.click();
        return;
      }
    }
  };

  // Add elements to DOM
  dom.appendChild(searchField);
  dom.appendChild(togglesContainer);
  dom.appendChild(prevButton);
  dom.appendChild(nextButton);
  dom.appendChild(selectButton);

  // Add replace elements if not read-only
  if (!isReadOnly) {
    dom.appendChild(replaceField);
    dom.appendChild(replaceButton);
    dom.appendChild(replaceAllButton);
  }

  dom.appendChild(closeButton);

  // Sync query state externally (e.g., when pressing Mod-d to select next)
  function setQuery(q: SearchQuery) {
    query = q;
    searchField.value = q.search;
    replaceField.value = q.replace;
    const wasCaseActive = caseToggle.classList.contains('cm-search-toggle-active');
    if (q.caseSensitive !== wasCaseActive) {
      caseToggle.classList.toggle('cm-search-toggle-active');
      caseToggle.setAttribute('aria-pressed', String(q.caseSensitive));
    }
    const wasWordActive = wordToggle.classList.contains('cm-search-toggle-active');
    if (q.wholeWord !== wasWordActive) {
      wordToggle.classList.toggle('cm-search-toggle-active');
      wordToggle.setAttribute('aria-pressed', String(q.wholeWord));
    }
    const wasRegexActive = regexToggle.classList.contains('cm-search-toggle-active');
    if (q.regexp !== wasRegexActive) {
      regexToggle.classList.toggle('cm-search-toggle-active');
      regexToggle.setAttribute('aria-pressed', String(q.regexp));
    }
  }

  // Panel update function - respond to external changes
  function update(update: ViewUpdate) {
    for (const tr of update.transactions) {
      for (const effect of tr.effects) {
        if (effect.is(setSearchQuery) && !effect.value.eq(query)) {
          setQuery(effect.value);
        }
      }
    }
  }

  // Mount callback - select search field on open
  function mount() {
    searchField.select();
  }

  return {
    dom,
    update,
    mount,
    get top() { return false; },
  };
}

/**
 * Custom search extension that replaces the default search panel with a styled version.
 *
 * Use this instead of `search()` from `@codemirror/search`.
 *
 * @param onSave - Optional callback invoked when Mod-s (save) is pressed while
 *   the search panel has focus. If not provided, the save shortcut is ignored.
 */
export function customSearchExtension(onSave?: () => void) {
  return [
    search({ createPanel: (view: EditorView) => createCustomSearchPanel(view, onSave) }),
  ];
}