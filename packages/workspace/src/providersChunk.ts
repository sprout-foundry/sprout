/**
 * Providers entry point for the library build.
 *
 * The views a host mounts read the web UI context stack (the adapter, the
 * events transport, notifications, the buffer/editor/pane/hotkey contexts,
 * the theme, the plugin and provider catalogs). `SproutProviders` is that
 * stack behind one wrapper, so a host does not assemble it by hand: it wraps
 * the views it composes and every context they need is present.
 *
 * It is its own entry point and chunk — the wrapper reaches the web UI
 * contexts, which a host loads alongside the views, not when it imports the
 * package's static entry. The `views` entry re-exports it, so a host that
 * composes the views gets the wrapper from the same surface.
 */
export {
  SproutProviders,
  type SproutProvidersProps,
} from "../../../webui/src/providers/SproutProviders";
