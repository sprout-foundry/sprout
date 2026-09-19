import type {
  AskUserRequestData,
  EditApprovalRequestData,
  PasswordRequestData,
  SecurityApprovalRequestData,
  SecurityPromptRequestData,
  ShellApprovalRequestData,
} from '@sprout/events';
import { notifyIfHidden } from '../../services/desktopNotify';
import { debugLog } from '../../utils/log';
import { appendCappedLog } from '../../utils/logCap';
import { parseSecurityAnalysis } from '../../utils/parseSecurityAnalysis';
import { createLogEntry, type EventHandlerContext } from '../webSocketEventHelpers';

// Handle security_approval_request event
export const handleSecurityApprovalRequest = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'warning';
  const data = (event.data ?? {}) as SecurityApprovalRequestData;
  if (data.status === 'responded') return;
  setState((prev) => ({
    securityApprovalRequest: {
      requestId: String(data.request_id || ''),
      toolName: String(data.tool_name || ''),
      riskLevel: String(data.risk_level || 'CAUTION'),
      reasoning: String(data.reasoning || ''),
      command: data.command != null ? String(data.command) : undefined,
      riskType: data.risk_type != null ? String(data.risk_type) : undefined,
      target: data.target != null ? String(data.target) : undefined,
      // SP-058: the server sends allow_options="true" in extras for
      // shell_command. Without it the dialog falls back to the legacy
      // Allow / Block pair and the Elevate / Always-approve actions are
      // unreachable. fs fields (kind/folder/path) drive the
      // filesystem-tier dialog (backend extras["kind"] etc.).
      allowOptions: data.allow_options === 'true',
      fsKind: data.kind === 'fs_external' || data.kind === 'fs_sensitive' ? data.kind : undefined,
      fsFolder: data.folder != null ? String(data.folder) : undefined,
      fsPath: data.path != null ? String(data.path) : data.target != null ? String(data.target) : undefined,
      // SP-124-2: LLM-derived analysis attached by the backend. Parse on
      // receive so the dialog can render the summary / recommendation
      // panel above the command.
      securityAnalysis: parseSecurityAnalysis(event.data),
    },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[security] Approval request:', data.tool_name, data.risk_level);
};

// Handle security_prompt_request event
export const handleSecurityPromptRequest = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'warning';
  const data = (event.data ?? {}) as SecurityPromptRequestData;
  if (data.status === 'responded') return;
  if (!data.prompt) return;
  setState((prev) => ({
    securityPromptRequest: {
      requestId: String(data.request_id || ''),
      prompt: String(data.prompt || ''),
      filePath: data.file_path != null ? String(data.file_path) : undefined,
      concern: data.concern != null ? String(data.concern) : undefined,
    },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[security] Prompt request:', data.file_path, data.concern);
};

// Handle ask_user_request event
export const handleAskUserRequest = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as AskUserRequestData;
  if (data.status === 'responded') return;
  if (data.status === 'cancelled') {
    setState((prev) => ({ askUserRequest: null }));
    return;
  }
  if (!data.question) return;
  setState((prev) => ({
    askUserRequest: {
      requestId: String(data.request_id || ''),
      question: String(data.question || ''),
      header: data.header,
      options: Array.isArray(data.options)
        ? data.options.filter((o) => o && typeof o.label === 'string' && o.label.length > 0)
        : undefined,
      multiSelect: Boolean(data.multi_select),
      default: data.default,
    },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[ask_user] Question:', data.question, 'options:', data.options?.length ?? 0);
};

// Handle edit_approval_request event (SP-072-3)
export const handleEditApprovalRequest = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'warning';
  const data = (event.data ?? {}) as EditApprovalRequestData;
  if (data.status === 'responded') return;
  if (!data.request_id || !data.file_path) return;

  const hunks = Array.isArray(data.hunks)
    ? data.hunks.map((h) => ({
        id: String(h.id || ''),
        oldStart: Number(h.old_start ?? 0),
        oldLines: Number(h.old_lines ?? 0),
        newStart: Number(h.new_start ?? 0),
        newLines: Number(h.new_lines ?? 0),
        lines: Array.isArray(h.lines)
          ? h.lines.map((l) => ({
              type: (l.type === 'add' || l.type === 'remove' ? l.type : 'context') as 'context' | 'add' | 'remove',
              content: String(l.content || ''),
            }))
          : [],
        addCount: Number(h.add_count ?? 0),
        delCount: Number(h.del_count ?? 0),
      }))
    : [];

  setState((prev) => ({
    editApprovalRequest: {
      requestId: String(data.request_id),
      filePath: String(data.file_path),
      unifiedDiff: data.unified_diff != null ? String(data.unified_diff) : undefined,
      hunks,
    },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[edit_approval] Request:', data.file_path, 'hunks:', hunks.length);
};

// Handle shell_approval_request event (SP-093-3)
export const handleShellApprovalRequest = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'warning';
  const data = (event.data ?? {}) as ShellApprovalRequestData;
  if (!data.request_id || !data.command) return;

  const parts = Array.isArray(data.parts)
    ? (data.parts as Array<Partial<ShellApprovalRequestData['parts'][number]>>).map((p) => ({
        id: String(p?.id || ''),
        text: String(p?.text || ''),
        kind: String(p?.kind || ''),
        semantic: String(p?.semantic || ''),
        risk: String(p?.risk || ''),
      }))
    : [];

  // SP-124-2: parse the JSON-encoded security_analysis field if present.
  // Silent fall-through on malformed JSON per the SP-124 "analyzer is
  // non-blocking" contract.
  const securityAnalysis = parseSecurityAnalysis(data);

  setState((prev) => ({
    shellApprovalRequest: {
      requestId: String(data.request_id),
      command: String(data.command),
      parts,
      unifiedView: data.unified_view != null ? String(data.unified_view) : '',
      riskLevel: String(data.risk_level || 'High'),
      securityAnalysis,
    },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[shell_approval] Request:', data.command, 'parts:', parts.length);
};

// Handle input_required event (SP-070-4: desktop notification)
export const handleInputRequired = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'info';

  // SP-070-4: desktop notification when tab is backgrounded
  notifyIfHidden('Sprout', 'Input required');

  setState((prev) => ({
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  debugLog('[input_required] Input required event received');
};

// Handle password_request event (SP-089-3). The backend's WebUI password
// prompter blocks until the user responds (or the timeout fires), so this
// handler must surface the dialog immediately. Response flows back through
// handlePasswordResponse in useSecurityHandlers (WS password_response).
export const handlePasswordRequest = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'system';
  logEntry.level = 'warning';
  const data = (event.data ?? {}) as PasswordRequestData;
  if (data.status === 'responded') return;
  if (!data.request_id) return;
  setState((prev) => ({
    passwordRequest: {
      requestId: String(data.request_id),
      command: String(data.command || ''),
      prompt: String(data.prompt || ''),
    },
    logs: appendCappedLog(prev.logs, logEntry),
  }));
  notifyIfHidden('Sprout', `Password required: ${data.command || 'shell command'}`);
  debugLog('[password] Prompt request:', data.command);
};

// Handle delegate_clarification_requested event (log-only; full UI is a follow-up).
export const handleDelegateClarificationRequested = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'tool';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as Record<string, unknown>;
  setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
  debugLog('[delegate] Clarification requested:', data);
};

// Handle delegate_clarification_responded event (log-only; full UI is a follow-up).
export const handleDelegateClarificationResponded = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'tool';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as Record<string, unknown>;
  setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
  debugLog('[delegate] Clarification responded:', data);
};
