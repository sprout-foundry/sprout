/**
 * Views entry point for the library build.
 *
 * The views a host composes (chat, agent changes, files, editor, preview,
 * the layout and the arrangement) as their own entry point and chunk. A host
 * loads them when it mounts them, not when it imports the package.
 */
// The app loads all of `@sprout/ui`'s CSS up front (its prebuilt
// stylesheet); the package bundles `@sprout/ui` from source and so carries
// only the CSS of the modules it reaches. The chat surface the web UI renders
// is styled by the chat panel's stylesheet, whose component the web UI does
// not import, so it is loaded here — first, as the app orders it.
import '../../ui/src/components/ChatPanel.css';

export * from '../../../webui/src/views/index';
export { ViewsLayout } from '../../../webui/src/views/ViewsLayout';

// The app's shell chrome (the header, the editor and context-panel columns,
// the pane layout) is styled by the app stylesheet, which the app imports at
// its root rather than from the components. The space shells render that
// chrome, so the package carries the stylesheet too — after the views, as the
// app orders it after its components.
import '../../../webui/src/App.css';
