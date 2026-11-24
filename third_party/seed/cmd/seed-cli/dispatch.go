package main

// rpcError represents a JSON-RPC error for the CLI protocol.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// dispatch handles a JSON-RPC method and returns the result or error to serialize.
func dispatch(state *cliState, method string, params map[string]interface{}) (map[string]interface{}, *rpcError) {
	switch method {
	case "agent.new":
		return state.newAgent(params)
	case "agent.run":
		return state.run(params)
	case "agent.runStream":
		return state.runStream(params)
	case "agent.interrupt":
		return state.interrupt()
	case "agent.resetInterrupt":
		return state.resetInterrupt()
	case "agent.exportState":
		return state.exportState()
	case "agent.importState":
		return state.importState(params)

	// Aliases for state.* namespace
	case "state.exportState":
		return state.exportState()
	case "state.importState":
		return state.importState(params)
	case "agent.setSystemPrompt":
		return state.setSystemPrompt(params)
	case "agent.steer":
		return state.steer(params)
	case "agent.steerSystem":
		return state.steerSystem(params)
	case "agent.pause":
		return state.pause()
	case "agent.resume":
		return state.resume()
	case "agent.isPaused":
		return state.isPaused()
	case "agent.checkpoints":
		return state.checkpoints()
	case "agent.state":
		return state.agentState()
	case "mock.addTextResponse":
		return state.addTextResponse(params)
	case "mock.addToolCallResponse":
		return state.addToolCallResponse(params)
	case "mock.addError":
		return state.addError(params)
	case "mock.addMalformedResponse":
		return state.addMalformedResponse(params)
	case "mock.addTextResponseWithFinish":
		return state.addTextResponseWithFinish(params)
	case "mock.addTool":
		return state.addTool(params)
	case "mock.addToolResult":
		return state.addToolResult(params)
	case "mock.reset":
		return state.reset()
	case "mock.callCount":
		return state.callCount()
	case "mock.lastRequest":
		return state.lastRequest()
	case "mock.withInfo":
		return state.withInfo(params)
	case "mock.withTokenEstimate":
		return state.withTokenEstimate(params)
	case "mock.withStreaming":
		return state.withStreaming()
	case "mock.addStreamChunks":
		return state.addStreamChunks(params)
	case "mock.blockOnCallN":
		return state.blockOnCallN(params)
	case "mock.unblock":
		return state.unblock()
	case "mock.executorCallCount":
		return state.executorCallCount()
	case "mock.executorLastCalls":
		return state.executorLastCalls()
	case "mock.blockUntil":
		return state.blockUntilHandler()
	case "mock.release":
		return state.releaseHandler(params)

	// State management (SP-016-1c)
	case "state.messages":
		return state.stateMessages()
	case "state.sessionId":
		return state.stateSessionID()
	case "state.setSessionId":
		return state.stateSetSessionID(params)
	case "state.ensureSessionId":
		return state.stateEnsureSessionID()
	case "state.tokens":
		return state.stateTokens()
	case "state.cost":
		return state.stateCost()
	case "state.addMessage":
		return state.stateAddMessage(params)
	case "state.len":
		return state.stateLen()
	case "state.clearCheckpoints":
		return state.stateClearCheckpoints()

	// Configuration (SP-016-1d)
	case "agent.setProvider":
		return state.setProvider(params)
	case "agent.setFlushCallback":
		return state.setFlushCallback()

	// Steering & Injection (SP-016-1e)
	case "agent.injectInput":
		return state.injectInput(params)

	// Checkpoints, Provider, Streaming (SP-016-1f)
	case "agent.providerInfo":
		return state.providerInfo()
	case "agent.estimateTokens":
		return state.estimateTokens(params)
	case "agent.streamingBuffer":
		return state.streamingBuffer()
	case "agent.reasoningBuffer":
		return state.reasoningBuffer()

	// Output Manager (SP-016-1g)
	case "output.setMetadata":
		return state.outputSetMetadata(params)
	case "output.getMetadata":
		return state.outputGetMetadata(params)
	case "output.flush":
		return state.outputFlush()
	case "output.reset":
		return state.outputReset()

	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}
