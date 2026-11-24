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

// The app's shell chrome is styled by stylesheets the app loads with its own
// chrome rather than from the components the space shells render: the project
// sidebar's (with the app's sidebar) and the header, editor and context-panel
// columns' (the app stylesheet, imported at its root). The composed space
// shell renders that chrome, so the package carries both — after the views,
// in the order the app loads them.
import '../../../webui/src/components/layered/Layered.css';
import '../../../webui/src/App.css';
