import { StateField, StateEffect, RangeSetBuilder } from '@codemirror/state';
import { gutter, GutterMarker } from '@codemirror/view';
import { Decoration, type DecorationSet, EditorView } from '@codemirror/view';
import { type DiffLineChange, parseGitDiff } from '../services/gitDiffParser';

// State effect to update diff info
const setDiffEffect = StateEffect.define<DiffLineChange[]>();

// State field storing per-line diff types. Provided directly to the
// EditorView.decorations facet — block decorations (Decoration.line)
// may NOT be provided via a ViewPlugin's `decorations` option, which
// throws "Block decorations may not be specified via plugins".
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
              class: `cm-diffLine${change.type.charAt(0).toUpperCase() + change.type.slice(1)}`,
            });
            builder.add(line.from, line.from, deco);
          }
        }

        return builder.finish();
      }
    }
    return tr.changes.empty ? decorations : decorations.map(tr.changes);
  },
  provide: (f) => EditorView.decorations.from(f),
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

// Create the gutter extension
export function diffGutter() {
  return [diffState, diffGutterExtension];
}

// Function to update the diff gutter with new diff text
export function updateDiffGutter(view: EditorView, diffText: string) {
  const changes = parseGitDiff(diffText);
  view.dispatch({
    effects: setDiffEffect.of(changes),
  });
}

// Function to clear the diff gutter
export function clearDiffGutter(view: EditorView) {
  view.dispatch({
    effects: setDiffEffect.of([]),
  });
}
