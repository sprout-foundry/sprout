import { gutter, GutterMarker } from '@codemirror/view';
import { StateField, StateEffect, RangeSetBuilder } from '@codemirror/state';
import { Decoration, DecorationSet, EditorView, ViewPlugin, ViewUpdate } from '@codemirror/view';
import { DiffLineChange, parseGitDiff } from '../services/gitDiffParser';

// State effect to update diff info
const setDiffEffect = StateEffect.define<DiffLineChange[]>();

// State field storing per-line diff types
const diffState = StateField.define<DecorationSet>({
  create() {
    return Decoration.none;
  },
  update(decorations, tr) {
    for (const effect of tr.effects) {
      if (effect.is(setDiffEffect)) {
        const changes = effect.value;
        const builder = new RangeSetBuilder<Decoration>();
        
        for (const change of changes) {
          const line = tr.state.doc.line(change.newLine + 1);
          if (line) {
            const deco = Decoration.line({
              class: `cm-diffLine${change.type.charAt(0).toUpperCase() + change.type.slice(1)}`
            });
            builder.add(line.from, line.from, deco);
          }
        }
        
        return builder.finish();
      }
    }
    return decorations;
  }
});

// Gutter extension that uses the diff state
const diffGutterExtension = gutter({
  class: 'cm-diffGutter',
  markers: (view) => {
    const decorations = view.state.field(diffState);
    const builder = new RangeSetBuilder<GutterMarker>();
    
    // Use the correct iteration pattern for RangeSet
    // cursor.next() is void; cursor.value is null at end
    const cursor = decorations.iter();
    while (cursor.value) {
      const deco = cursor.value;
      if (deco.spec?.class) {
        let markerType: 'added' | 'removed' | 'modified' | null = null;
        if (deco.spec.class.includes('cm-diffLineAdded')) markerType = 'added';
        else if (deco.spec.class.includes('cm-diffLineRemoved')) markerType = 'removed';
        else if (deco.spec.class.includes('cm-diffLineModified')) markerType = 'modified';
        
        if (markerType) {
          const marker = new DiffMarker(markerType);
          builder.add(cursor.from, cursor.from, marker);
        }
      }
      cursor.next();
    }
    
    return builder.finish();
  },
});

// Gutter marker class for diff indicators
class DiffMarker extends GutterMarker {
  constructor(private type: 'added' | 'removed' | 'modified') {
    super();
  }

  toDOM() {
    const el = document.createElement('div');
    el.className = `cm-diff-marker cm-diff-marker-${this.type}`;
    return el;
  }

  eq(other: GutterMarker) {
    return other instanceof DiffMarker && other.type === this.type;
  }
}

// View plugin to handle diff updates
const diffUpdatePlugin = ViewPlugin.fromClass(class {
  constructor(public view: EditorView) {}
  
  update(update: ViewUpdate) {
    // Diff state is managed by StateField, no need for manual updates
  }
}, {
  decorations: (pluginInstance) => pluginInstance.view.state.field(diffState)
});

// Create the gutter extension
export function diffGutter() {
  return [
    diffState,
    diffUpdatePlugin,
    diffGutterExtension
  ];
}

// Function to update the diff gutter with new diff text
export function updateDiffGutter(view: EditorView, diffText: string) {
  const changes = parseGitDiff(diffText);
  view.dispatch({
    effects: setDiffEffect.of(changes)
  });
}

// Function to clear the diff gutter
export function clearDiffGutter(view: EditorView) {
  view.dispatch({
    effects: setDiffEffect.of([])
  });
}
