/**
 * views entry point — runtime + type smoke.
 *
 * Runtime: every documented view export is defined (components/functions).
 * Type-level: minimal `Props`-annotated values prove the exported view types
 * are real, usable types from this entry (a missing/renamed export breaks
 * `tsc` here, not just at the consumer).
 */
import { describe, expect, it } from 'vitest';
import { DEFAULT_VIEWS_ARRANGEMENT, SLOT_ORDER, VIEWS_BY_KIND, resolveViewsArrangement } from './ViewsLayout';
import type { ExampleEmbeddingProps, ViewKind, ViewSlot, ViewsArrangement, ViewsLayoutProps } from './ViewsLayout';
import {
  AgentChangesPanel,
  AgentEscalationBridge,
  ChatView,
  Editor,
  EscalationListener,
  FileTree,
  PreviewPane,
  PreviewPanel,
  ExampleEmbedding,
  SproutWorkspace,
  ViewsLayout,
  createEmptyChatState,
  escalateCommand,
  getEscalationPolicy,
  installEscalationBridge,
  setEscalationPolicy,
  useEscalationTriggers,
  usePreviewStatus,
  copy,
  formatCopy,
  installCopy,
  resetCopyForTests,
  DEFAULT_COPY,
  COPY_KEYS,
  ESCALATION_TRIGGER_EVENT,
  useWorkspaceMode,
} from './index';
import type {
  AgentChangesPanelProps,
  AgentEscalationBridgeProps,
  ChatProps,
  ConsentAnswer,
  ConsentContext,
  ConsentDecision,
  CopyKey,
  CopyOverrides,
  EditorProps,
  EscalationBridgeOptions,
  EscalationHost,
  EscalationPolicy,
  EscalationResult,
  EscalationTriggerEvent,
  FileTreeProps,
  PreviewPaneProps,
  PreviewPanelProps,
  SproutProject,
  SproutWorkspaceProps,
  UseEscalationTriggersOptions,
  UsePreviewStatusReturn,
  UseWorkspaceModeResult,
} from './index';

// ── Runtime: all view exports are defined ─────────────────────────────

// React components are functions — except forwardRef components, which are
// objects tagged with a react.$$typeof symbol (e.g. FileTree).
const isReactComponent = (v: unknown): boolean =>
  typeof v === 'function' ||
  (typeof v === 'object' && v !== null && typeof (v as { $$typeof?: unknown }).$$typeof === 'symbol');

describe('views entry exports', () => {
  it('exports the chat view as a component', () => {
    expect(isReactComponent(ChatView)).toBe(true);
  });

  it('exports the changes view as a component', () => {
    expect(isReactComponent(AgentChangesPanel)).toBe(true);
  });

  it('exports the files views as components', () => {
    expect(isReactComponent(FileTree)).toBe(true);
    expect(isReactComponent(Editor)).toBe(true);
  });

  it('exports the preview pane and panel as components', () => {
    expect(isReactComponent(PreviewPane)).toBe(true);
    expect(isReactComponent(PreviewPanel)).toBe(true);
  });

  it('exports the preview lifecycle hook as a function', () => {
    expect(typeof usePreviewStatus).toBe('function');
  });

  // ── Layout configuration ──────────────────────────────────────────────

  it('exports the layout + example embedding as components', () => {
    expect(isReactComponent(ViewsLayout)).toBe(true);
    expect(isReactComponent(ExampleEmbedding)).toBe(true);
  });

  it('exports the arrangement resolver as a function', () => {
    expect(typeof resolveViewsArrangement).toBe('function');
  });

  it('defines the default arrangement, slot order, and the kind registry', () => {
    expect(DEFAULT_VIEWS_ARRANGEMENT).toBeDefined();
    expect(SLOT_ORDER).toEqual(['left', 'center', 'right', 'overlay']);
    expect(Object.keys(VIEWS_BY_KIND).sort()).toEqual(
      ['chat', 'changes', 'editor', 'fileTree', 'previewPane', 'previewPanel'].sort(),
    );
    // Each registered kind maps to something renderable.
    for (const kind of Object.keys(VIEWS_BY_KIND) as ViewKind[]) {
      expect(VIEWS_BY_KIND[kind]).toBeDefined();
    }
  });

  // ── Copy keys (the embedding's wording seam) ─────────────────────────

  it('exports the copy registry API for the embedding to install wording', () => {
    expect(typeof copy).toBe('function');
    expect(typeof formatCopy).toBe('function');
    expect(typeof installCopy).toBe('function');
    expect(typeof resetCopyForTests).toBe('function');
    expect(COPY_KEYS.length).toBeGreaterThan(0);
    expect(copy('app.name')).toBe(DEFAULT_COPY['app.name']);
  });

  it('installs an override through the entry export, then resets', () => {
    const overrides: CopyOverrides = { 'app.name': 'acme' };
    const key: CopyKey = 'app.name';
    installCopy(overrides);
    expect(copy(key)).toBe('acme');
    resetCopyForTests();
    expect(copy(key)).toBe('sprout');
  });

  it('exports the workspace composition and its companion hook', () => {
    expect(isReactComponent(SproutWorkspace)).toBe(true);
    expect(typeof useWorkspaceMode).toBe('function');
  });

  it('exports the chat unit and the empty chat state a composition starts from', () => {
    expect(typeof createEmptyChatState).toBe('function');
    expect(createEmptyChatState().messages).toEqual([]);
  });

  // ── The escalation seam ──────────────────────────────────────────────

  it('exports the escalation bridge + listener as components', () => {
    expect(isReactComponent(AgentEscalationBridge)).toBe(true);
    expect(isReactComponent(EscalationListener)).toBe(true);
  });

  it('exports the bridge installer and the policy accessors as functions', () => {
    expect(typeof installEscalationBridge).toBe('function');
    expect(typeof escalateCommand).toBe('function');
    expect(typeof getEscalationPolicy).toBe('function');
    expect(typeof setEscalationPolicy).toBe('function');
  });

  it('exports the trigger detector and the event name the listener listens for', () => {
    expect(typeof useEscalationTriggers).toBe('function');
    expect(ESCALATION_TRIGGER_EVENT).toBe('sprout:escalation-trigger');
  });
});

// ── Type-level: the exported props are real, usable types ─────────────

describe('views entry typed props', () => {
  it('accepts a minimal PreviewPaneProps value', () => {
    const paneProps: PreviewPaneProps = {
      status: 'starting',
      title: 't',
      url: 'http://localhost:1',
      onRestart: () => undefined,
    };
    expect(paneProps.status).toBe('starting');
  });

  it('accepts a minimal PreviewPanelProps value', () => {
    const panelProps: PreviewPanelProps = { open: true, onClose: () => undefined };
    expect(panelProps.open).toBe(true);
  });

  it('accepts a minimal ChatProps value', () => {
    const chatProps: ChatProps = {
      messages: [{ id: 'm1', type: 'user', content: 'hello', timestamp: new Date(0) }],
      onSendMessage: () => undefined,
      onQueueMessage: () => undefined,
      queuedMessagesCount: 0,
      inputValue: '',
      onInputChange: () => undefined,
    };
    expect(chatProps.queuedMessagesCount).toBe(0);
  });

  it('accepts a minimal AgentChangesPanelProps value', () => {
    const changesProps: AgentChangesPanelProps = {
      onAskAgent: (_filePath: string) => undefined,
    };
    expect(typeof changesProps.onAskAgent).toBe('function');
  });

  it('accepts minimal FileTreeProps and EditorProps values', () => {
    const treeProps: FileTreeProps = { onFileSelect: () => undefined };
    const editorProps: EditorProps = { value: 'const x = 1;', language: 'javascript' };
    expect(typeof treeProps.onFileSelect).toBe('function');
    expect(editorProps.value).toBe('const x = 1;');
  });

  it('accepts a minimal UsePreviewStatusReturn value', () => {
    const hookReturn: UsePreviewStatusReturn = {
      status: 'stopped',
      reloadKey: 0,
      start: () => undefined,
      restart: () => undefined,
      stop: () => undefined,
    };
    expect(hookReturn.status).toBe('stopped');
  });

  it('accepts minimal SproutWorkspace + useWorkspaceMode values', () => {
    const project: SproutProject = { id: '/ws', root: '/ws' };
    const workspaceProps: SproutWorkspaceProps = { project, space: 'code', providers: 'ambient' };
    const composedProps: SproutWorkspaceProps = {
      project,
      space: 'code',
      layout: { center: ['chat'] },
      viewProps: { chat: { inputValue: 'host' } },
      chatInitialState: createEmptyChatState(),
      chatFetch: (async () => new Response()) as unknown as typeof fetch,
    };
    const modeResult: UseWorkspaceModeResult = {
      mode: {
        id: 'code',
        label: 'Code',
        icon: (() => null) as never,
        hint: '',
        available: () => true,
        Shell: () => null,
      },
      modes: [],
      select: () => undefined,
      canSwitch: false,
    };
    expect(workspaceProps.providers).toBe('ambient');
    expect(modeResult.canSwitch).toBe(false);
    expect(composedProps.viewProps?.chat).toBeDefined();
    expect(composedProps.chatInitialState?.messages).toEqual([]);
  });

  // ── Layout configuration types ────────────────────────────────────────

  it('accepts minimal arrangement + layout + example-embedding values', () => {
    const slot: ViewSlot = 'left';
    const kind: ViewKind = 'chat';
    const arrangement: ViewsArrangement = { center: [kind], overlay: [] };
    const layoutProps: ViewsLayoutProps = {
      arrangement,
      props: { chat: { inputValue: 'hi' } },
      className: 'host-shell',
    };
    const exampleProps: ExampleEmbeddingProps = {
      chat: {
        messages: [{ id: 'm1', type: 'user', content: 'hello', timestamp: new Date(0) }],
        onSendMessage: () => undefined,
        onQueueMessage: () => undefined,
        queuedMessagesCount: 0,
        inputValue: '',
        onInputChange: () => undefined,
      },
      preview: { open: true, onClose: () => undefined },
      arrangement: { left: [] },
    };
    expect(slot).toBe('left');
    expect(arrangement.center).toEqual(['chat']);
    expect(layoutProps.className).toBe('host-shell');
    expect(exampleProps.arrangement).toEqual({ left: [] });
    // The resolver round-trips the minimal value.
    expect(resolveViewsArrangement({ center: [kind] }).center).toEqual(['chat']);
  });

  it('accepts minimal escalation-seam values', () => {
    const bridgeProps: AgentEscalationBridgeProps = { repoURL: 'https://github.com/acme/app' };
    const decision: ConsentDecision = 'once';
    const answer: ConsentAnswer = { decision, host: { kind: 'cloud' } };
    const context: ConsentContext = { notice: 'your runner is offline', unavailableRunnerId: 'r1' };
    const policy: EscalationPolicy = 'ask';
    const host: EscalationHost = { kind: 'runner', runnerId: 'r1', name: 'Mac mini' };
    const result: EscalationResult = { ran: true, stdout: 'ok', exitCode: 0 };
    const bridgeOptions: EscalationBridgeOptions = {
      repoURL: 'https://github.com/acme/app',
      requestConsent: async () => 'deny',
    };
    const trigger: EscalationTriggerEvent = {
      id: 'wasm-command-unavailable-1a2b',
      reason: 'command_unavailable_in_browser',
      severity: 'blocking',
      message: 'needs a real runtime',
      command: 'go build ./...',
    };
    const triggerOptions: UseEscalationTriggersOptions = { repoURL: 'https://github.com/acme/app' };
    expect(bridgeProps.repoURL).toContain('acme');
    expect(answer.decision).toBe('once');
    expect(context.unavailableRunnerId).toBe('r1');
    expect(policy).toBe('ask');
    expect(host.kind).toBe('runner');
    expect(result.ran).toBe(true);
    expect(bridgeOptions.requestConsent).toBeDefined();
    expect(trigger.severity).toBe('blocking');
    expect(triggerOptions.repoURL).toContain('acme');
  });
});
