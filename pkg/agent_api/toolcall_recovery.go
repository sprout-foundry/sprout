package api

// RecoverInlineToolCalls runs text-based tool-call recovery on a ChatResponse
// for models that emit tool calls inline in message content instead of using
// the structured tool_calls field. This is called from the streaming path
// after the response is finalized, where the unified.go recovery hooks don't
// run (those only cover the non-streaming and unified-provider paths).
//
// Tries Mistral-family `[TOOL_CALLS]` format first, then LFM2's Pythonic
// `[func(args)]` format. Only runs when no structured tool_calls were parsed
// and tools were offered.
func RecoverInlineToolCalls(resp *ChatResponse, tools []Tool) {
	if resp == nil || len(tools) == 0 {
		return
	}
	for i := range resp.Choices {
		if len(resp.Choices[i].Message.ToolCalls) > 0 {
			continue
		}
		content := resp.Choices[i].Message.Content
		if recovered, rest, ok := RecoverMistralToolCalls(content); ok {
			resp.Choices[i].Message.ToolCalls = recovered
			resp.Choices[i].Message.Content = rest
		} else if recovered, rest, ok := RecoverLFM2ToolCalls(content); ok {
			resp.Choices[i].Message.ToolCalls = recovered
			resp.Choices[i].Message.Content = rest
		}
	}
}
