import type { SubagentActivityData, ToolEndData, ToolStartData } from '@sprout/events';
import type { Message, SubagentActivity, ToolExecution } from '@sprout/ui';
import { debugLog } from '../../utils/log';
import { appendCappedLog } from '../../utils/logCap';
import { generateMessageId } from '../../utils/messageId';
import { createLogEntry, type EventHandlerContext, getToolCallId } from '../webSocketEventHelpers';

// Handle tool_start event
export const handleToolStart = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'tool';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as ToolStartData;
  const toolCallID = String(data.tool_call_id || '');
  const toolName = String(data.tool_name || 'unknown_tool');
  const rawArgs = data.arguments != null ? String(data.arguments) : undefined;
  const displayName = String(data.display_name || toolName);
  const persona = typeof data.persona === 'string' ? data.persona : undefined;
  const isSubagent = !!data.is_subagent;
  const subagentType: ToolExecution['subagentType'] =
    data.subagent_type === 'parallel' ? 'parallel' : isSubagent ? 'single' : undefined;
  const depth = Number((event.data as Record<string, unknown>)?.subagent_depth ?? 0);

  setState((prev) => {
    // When a tool starts, insert a marker into the assistant message content
    // so parseMessageSegments can interleave the tool badge at the correct
    // position in the text flow. Without this, all tool badges pile up at the
    // end of the message because there's no positional information linking
    // them to where in the text the tool was called.
    //
    // Never mark a subagent-run message: its content renders inside the
    // subagent's collapsible block, so a primary-agent tool badge inserted
    // there looks like the subagent's tool call. When the last message is a
    // subagent run, attach the marker to a fresh primary assistant message
    // instead (mirrors the start-of-turn case where streaming will append
    // into it).
    const lastMsg = prev.messages[prev.messages.length - 1];
    let messagesWithToolMarker: Message[];
    if (lastMsg && lastMsg.type === 'assistant' && lastMsg.isSubagentRun) {
      messagesWithToolMarker = [
        ...prev.messages,
        {
          id: generateMessageId(),
          type: 'assistant',
          content: '\n[executing tool [' + toolName + ']]\n',
          timestamp: new Date(),
        },
      ];
    } else {
      messagesWithToolMarker = prev.messages.map((msg, idx) => {
        if (idx === prev.messages.length - 1 && msg.type === 'assistant') {
          const trimmed = msg.content.replace(/\n+$/, '');
          return { ...msg, content: trimmed + '\n[executing tool [' + toolName + ']]\n' };
        }
        return msg;
      });
    }

    const existingIdx = prev.toolExecutions.findIndex((t) => (getToolCallId(t.details) || t.id) === toolCallID);
    const addToolRefToMessage = (messages: Message[], toolId: string) => {
      for (let i = messages.length - 1; i >= 0; i -= 1) {
        const msg = messages[i];
        // Skip subagent-run messages — the badge would render inside the
        // subagent's collapsible block.
        if (msg.type !== 'assistant' || msg.isSubagentRun) continue;
        const toolRefs = Array.isArray(msg.toolRefs) ? [...msg.toolRefs] : [];
        if (!toolRefs.some((ref) => ref.toolId === toolId)) {
          toolRefs.push({ toolId, toolName, label: displayName, parallel: subagentType === 'parallel' || undefined });
          messages[i] = { ...msg, toolRefs };
          return;
        }
      }
    };

    if (existingIdx >= 0) {
      const updated = [...prev.toolExecutions];
      updated[existingIdx] = {
        ...updated[existingIdx],
        tool: toolName,
        status: 'started',
        startTime: updated[existingIdx].startTime,
        message: displayName,
        arguments: updated[existingIdx].arguments || rawArgs,
        details: event.data,
        persona: updated[existingIdx].persona || persona,
        subagentType: updated[existingIdx].subagentType || subagentType,
        depth: updated[existingIdx].depth ?? (depth > 0 ? depth : undefined),
      };
      const messages = [...messagesWithToolMarker];
      addToolRefToMessage(messages, updated[existingIdx].id);
      return { messages, toolExecutions: updated, logs: appendCappedLog(prev.logs, logEntry) };
    }

    const newTool: ToolExecution = {
      id: toolCallID || `${toolName}-${Date.now()}`,
      tool: toolName,
      status: 'started',
      message: displayName,
      startTime: new Date(),
      details: event.data,
      arguments: rawArgs,
      persona,
      subagentType,
      depth: depth > 0 ? depth : undefined,
      queryId: prev.queryCount,
    };
    const messages = [...messagesWithToolMarker];
    addToolRefToMessage(messages, newTool.id);
    return { messages, toolExecutions: [...prev.toolExecutions, newTool], logs: appendCappedLog(prev.logs, logEntry) };
  });
  debugLog('[tool] Tool start:', data.tool_name);
};

// Handle tool_end event
export const handleToolEnd = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'tool';
  const data = (event.data ?? {}) as ToolEndData;
  logEntry.level = data.status === 'failed' ? 'error' : 'info';
  const toolCallID = String(data.tool_call_id || '');
  const status: ToolExecution['status'] = data.status === 'failed' ? 'error' : 'completed';
  const result = data.result != null ? String(data.result) : undefined;
  const error = data.error != null ? String(data.error) : undefined;

  setState((prev) => {
    let matched = false;
    const updatedExecutions = prev.toolExecutions.map((t) => {
      const existingID = getToolCallId(t.details) || t.id;
      const match = toolCallID && existingID === toolCallID;
      if (!match) {
        const nameMatch = !toolCallID && t.tool === data.tool_name && !t.endTime;
        if (!nameMatch) return t;
      }
      matched = true;
      return {
        ...t,
        status,
        endTime: new Date(),
        result: t.result || result || error,
        details: event.data,
        arguments: t.arguments,
      };
    });

    if (!matched) {
      const fallbackExecution: ToolExecution = {
        id: toolCallID || `${data.tool_name || 'tool'}-${Date.now()}`,
        tool: String(data.tool_name || 'unknown_tool'),
        status,
        message: String(data.display_name || data.tool_name || 'Tool'),
        startTime: new Date(),
        endTime: new Date(),
        details: event.data,
        arguments: data.arguments != null ? String(data.arguments) : undefined,
        result: result || error,
      };
      return {
        toolExecutions: [...prev.toolExecutions, fallbackExecution],
        logs: appendCappedLog(prev.logs, logEntry),
      };
    }

    const messagesAfterTool = prev.messages.map((msg, idx) => {
      if (idx === prev.messages.length - 1 && msg.type === 'assistant' && msg.content && !msg.content.endsWith('\n')) {
        return { ...msg, content: msg.content + '\n' };
      }
      return msg;
    });
    return {
      messages: messagesAfterTool,
      toolExecutions: updatedExecutions,
      logs: appendCappedLog(prev.logs, logEntry),
    };
  });
  debugLog('[tool] Tool end:', data.tool_name, data.status);
};

// Handle subagent_activity event
export const handleSubagentActivity = (ctx: EventHandlerContext): void => {
  const { event, setState } = ctx;
  const logEntry = createLogEntry(event);
  logEntry.category = 'tool';
  logEntry.level = 'info';
  const data = (event.data ?? {}) as SubagentActivityData;
  const activity: SubagentActivity = {
    id: String(event.id || `${Date.now()}-${Math.random()}`),
    toolCallId: String(data.tool_call_id || ''),
    toolName: String(data.tool_name || 'run_subagent'),
    phase:
      data.phase === 'spawn' || data.phase === 'complete' ? (data.phase as 'spawn' | 'complete' | 'output') : 'output',
    message: String(data.message || '').trim(),
    timestamp: new Date(),
    taskId: typeof data.task_id === 'string' ? data.task_id : undefined,
    persona: typeof data.persona === 'string' ? data.persona : undefined,
    isParallel: data.is_parallel === true,
    provider: typeof data.provider === 'string' ? data.provider : undefined,
    model: typeof data.model === 'string' ? data.model : undefined,
    taskCount: typeof data.task_count === 'number' ? data.task_count : undefined,
    failures: typeof data.failures === 'number' ? data.failures : undefined,
    status:
      typeof data.status === 'string' ? (data.status as 'queued' | 'started' | 'completed' | 'cancelled') : undefined,
    reason: typeof data.reason === 'string' ? data.reason : undefined,
    tokensUsed: typeof data.tokens_used === 'number' ? data.tokens_used : undefined,
    elapsedMs: typeof data.elapsed_ms === 'number' ? data.elapsed_ms : undefined,
  };

  if (!activity.message) {
    setState((prev) => ({ logs: appendCappedLog(prev.logs, logEntry) }));
    return;
  }

  // Inline subagent rendering: instead of (or in addition to) the footer
  // activity feed, render subagent output as collapsible sections inline
  // in the chat message flow — similar to reasoning blocks. The subagent
  // message ID is derived from the toolCallId so spawn/output/complete
  // events all target the same message row. Output lines accumulate in
  // the `reasoning` field (rendered as a Collapsible by MessageItem).
  const subagentMsgId = `subagent-${activity.toolCallId || activity.persona || activity.id}`;

  setState((prev) => {
    const newSubagentActivities = [...prev.subagentActivities, activity].slice(-500);

    if (activity.phase === 'spawn') {
      // Spawn: create a new assistant-type message for the subagent run.
      // If one already exists (e.g. re-spawn after reconnect), don't
      // duplicate — just log.
      const existing = prev.messages.find((m) => m.id === subagentMsgId);
      if (existing) {
        return { subagentActivities: newSubagentActivities, logs: appendCappedLog(prev.logs, logEntry) };
      }
      const newMsg: Message = {
        id: subagentMsgId,
        type: 'assistant',
        content: '',
        reasoning: '',
        timestamp: new Date(),
        persona: activity.persona,
        subagentDepth: 1,
        isSubagentRun: true,
        subagentRunComplete: false,
        subagentPersona: activity.persona,
      };
      return {
        messages: [...prev.messages, newMsg],
        subagentActivities: newSubagentActivities,
        logs: appendCappedLog(prev.logs, logEntry),
      };
    }

    if (activity.phase === 'output') {
      // Output: append the message text to the matching subagent message's
      // reasoning field (which renders as collapsible content).
      const msgIdx = prev.messages.findIndex((m) => m.id === subagentMsgId);
      if (msgIdx < 0) {
        // No matching message yet (e.g. output arrived before spawn, or
        // was evicted by trimMessages). Still track the activity for the
        // Subagents tab.
        return { subagentActivities: newSubagentActivities, logs: appendCappedLog(prev.logs, logEntry) };
      }
      const newMessages = [...prev.messages];
      const existingReasoning = newMessages[msgIdx].reasoning || '';
      newMessages[msgIdx] = {
        ...newMessages[msgIdx],
        reasoning: existingReasoning + activity.message + '\n',
      };
      return {
        messages: newMessages,
        subagentActivities: newSubagentActivities,
        logs: appendCappedLog(prev.logs, logEntry),
      };
    }

    if (activity.phase === 'complete') {
      // Complete: mark the subagent message as done. Optionally append
      // the completion message (e.g. "Done. Modified 2 files.") to the
      // reasoning content so it's visible in the collapsible.
      const msgIdx = prev.messages.findIndex((m) => m.id === subagentMsgId);
      if (msgIdx < 0) {
        return { subagentActivities: newSubagentActivities, logs: appendCappedLog(prev.logs, logEntry) };
      }
      const newMessages = [...prev.messages];
      const existingReasoning = newMessages[msgIdx].reasoning || '';
      newMessages[msgIdx] = {
        ...newMessages[msgIdx],
        subagentRunComplete: true,
        reasoning: existingReasoning + activity.message + '\n',
      };
      return {
        messages: newMessages,
        subagentActivities: newSubagentActivities,
        logs: appendCappedLog(prev.logs, logEntry),
      };
    }

    // step or unknown phase — just track the activity
    return { subagentActivities: newSubagentActivities, logs: appendCappedLog(prev.logs, logEntry) };
  });
};
