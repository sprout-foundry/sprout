export type ChatMessageLike = {
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: Date;
  /** Inline subagent-run marker — see Message type. Completion content must
   *  not be written into these messages. */
  isSubagentRun?: boolean;
};

export const ensureCompletedAssistantMessage = <T extends ChatMessageLike>(
  messages: T[],
  response: unknown,
  createAssistantMessage: (responseText: string) => T,
): T[] => {
  if (typeof response !== 'string' || !response.trim()) {
    return messages;
  }

  const updatedMessages = [...messages];
  let lastUserIndex = -1;

  for (let i = updatedMessages.length - 1; i >= 0; i -= 1) {
    if (updatedMessages[i].type === 'user') {
      lastUserIndex = i;
      break;
    }
  }

  let assistantIndex = -1;
  for (let i = updatedMessages.length - 1; i > lastUserIndex; i -= 1) {
    const m = updatedMessages[i];
    // Skip inline subagent-run messages: they represent a delegated run,
    // not the primary agent's response. Writing the completion into one of
    // them (or letting a later chunk append into it) makes primary-agent
    // output render inside the subagent's collapsible block.
    if (m.type === 'assistant' && m.isSubagentRun) continue;
    if (m.type === 'assistant') {
      assistantIndex = i;
      break;
    }
  }

  if (assistantIndex === -1) {
    updatedMessages.push(createAssistantMessage(response));
    return updatedMessages;
  }

  const assistantMessage = updatedMessages[assistantIndex];
  const streamedContent = (assistantMessage.content || '').trim();
  if (streamedContent) {
    // Streaming content usually wins (it's the authoritative incremental
    // build). But if the server's final response is substantially longer
    // (>20%), streaming was likely interrupted (disconnection, buffer loss)
    // and the server has the complete text. Replace with the server response
    // so the user doesn't see a truncated message.
    if (response.trim().length > streamedContent.length * 1.2) {
      updatedMessages[assistantIndex] = {
        ...assistantMessage,
        content: response,
      };
      return updatedMessages;
    }
    return messages;
  }

  updatedMessages[assistantIndex] = {
    ...assistantMessage,
    content: response,
  };
  return updatedMessages;
};
