import { EditorView, KeyBinding } from '@codemirror/view';
import { HotkeyPreset } from '../contexts/HotkeyContext';

interface EditorHotkeyActions {
  onSave: () => void;
  onGoToLine: () => void;
  onToggleLineNumbers: () => void;
}

function hasSingleSelection(view: EditorView): boolean {
  return view.state.selection.ranges.length === 1;
}

function duplicateCurrentLine(view: EditorView, direction: 'up' | 'down' = 'down'): boolean {
  if (!hasSingleSelection(view)) {
    return false;
  }

  const cursor = view.state.selection.main.head;
  const line = view.state.doc.lineAt(cursor);
  const lineText = line.text;
  const insertText = lineText + '\n';

  if (direction === 'up') {
    view.dispatch({
      changes: { from: line.from, insert: insertText },
      selection: { anchor: cursor + insertText.length },
      scrollIntoView: true,
    });
    return true;
  }

  const insertPos = line.to;
  const prefix = line.to === view.state.doc.length ? '\n' : '';
  view.dispatch({
    changes: { from: insertPos, insert: prefix + lineText },
    selection: { anchor: cursor + prefix.length + lineText.length + (line.to === view.state.doc.length ? 1 : 0) },
    scrollIntoView: true,
  });
  return true;
}

function deleteCurrentLine(view: EditorView): boolean {
  if (!hasSingleSelection(view)) {
    return false;
  }

  const cursor = view.state.selection.main.head;
  const line = view.state.doc.lineAt(cursor);
  const isLastLine = line.to === view.state.doc.length;

  let from = line.from;
  let to = line.to;

  if (!isLastLine) {
    to += 1;
  } else if (line.from > 0) {
    from -= 1;
  }

  view.dispatch({
    changes: { from, to, insert: '' },
    selection: { anchor: Math.max(0, from) },
    scrollIntoView: true,
  });

  return true;
}

function moveCurrentLine(view: EditorView, direction: 'up' | 'down'): boolean {
  if (!hasSingleSelection(view)) {
    return false;
  }

  const cursor = view.state.selection.main.head;
  const line = view.state.doc.lineAt(cursor);

  if (direction === 'up') {
    if (line.number <= 1) {
      return true;
    }

    const prevLine = view.state.doc.line(line.number - 1);
    const from = prevLine.from;
    const to = line.to;
    const swapped = `${line.text}\n${prevLine.text}`;
    const newCursor = Math.max(from, cursor - (prevLine.length + 1));

    view.dispatch({
      changes: { from, to, insert: swapped },
      selection: { anchor: newCursor },
      scrollIntoView: true,
    });
    return true;
  }

  if (line.number >= view.state.doc.lines) {
    return true;
  }

  const nextLine = view.state.doc.line(line.number + 1);
  const from = line.from;
  const to = nextLine.to;
  const swapped = `${nextLine.text}\n${line.text}`;
  const newCursor = Math.min(from + swapped.length, cursor + (nextLine.length + 1));

  view.dispatch({
    changes: { from, to, insert: swapped },
    selection: { anchor: newCursor },
    scrollIntoView: true,
  });

  return true;
}

export function getHotkeyPresetKeymap(
  preset: HotkeyPreset,
  actions: EditorHotkeyActions
): KeyBinding[] {
  const coreBindings: KeyBinding[] = [
    {
      key: 'Mod-s',
      preventDefault: true,
      run: () => {
        actions.onSave();
        return true;
      },
    },
    {
      key: 'Mod-g',
      preventDefault: true,
      run: () => {
        actions.onGoToLine();
        return true;
      },
    },
  ];

  if (preset === 'vscode') {
    return [
      ...coreBindings,
      {
        key: 'Alt-ArrowUp',
        preventDefault: true,
        run: (view) => moveCurrentLine(view, 'up'),
      },
      {
        key: 'Alt-ArrowDown',
        preventDefault: true,
        run: (view) => moveCurrentLine(view, 'down'),
      },
      {
        key: 'Shift-Alt-ArrowDown',
        preventDefault: true,
        run: (view) => duplicateCurrentLine(view, 'down'),
      },
      {
        key: 'Shift-Alt-ArrowUp',
        preventDefault: true,
        run: (view) => duplicateCurrentLine(view, 'up'),
      },
      {
        key: 'Mod-l',
        preventDefault: true,
        run: () => {
          actions.onToggleLineNumbers();
          return true;
        },
      },
    ];
  }

  if (preset === 'webstorm') {
    return [
      ...coreBindings,
      {
        key: 'Mod-d',
        preventDefault: true,
        run: (view) => duplicateCurrentLine(view, 'down'),
      },
      {
        key: 'Mod-y',
        preventDefault: true,
        run: (view) => deleteCurrentLine(view),
      },
      {
        key: 'Shift-Alt-ArrowUp',
        preventDefault: true,
        run: (view) => moveCurrentLine(view, 'up'),
      },
      {
        key: 'Shift-Alt-ArrowDown',
        preventDefault: true,
        run: (view) => moveCurrentLine(view, 'down'),
      },
      {
        key: 'Mod-l',
        preventDefault: true,
        run: () => {
          actions.onToggleLineNumbers();
          return true;
        },
      },
    ];
  }

  return [
    ...coreBindings,
    {
      key: 'Mod-l',
      preventDefault: true,
      run: () => {
        actions.onToggleLineNumbers();
        return true;
      },
    },
  ];
}
