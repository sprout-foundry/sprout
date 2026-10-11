/**
 * Ask the workspace to open a file in its editor. The composed workspace and
 * the standalone app both handle the `sprout:open-in-editor` request, so a
 * view rendered without an explicit open handler still reaches the editor.
 */
export function requestOpenInEditor(path: string, line?: number): void {
  window.dispatchEvent(new CustomEvent('sprout:open-in-editor', { detail: { path, lineNumber: line } }));
}
