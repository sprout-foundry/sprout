/**
 * The provider wrapper the views need.
 *
 * A host that composes the individual views wraps them in `SproutProviders`
 * so it does not assemble the web UI context stack by hand (the adapter,
 * events transport, notifications, buffer/editor/pane/hotkey contexts, theme,
 * plugin and provider catalogs). See `providers/SproutProviders.tsx` for the
 * stack and the order.
 */
export { SproutProviders, type SproutProvidersProps } from './SproutProviders';
