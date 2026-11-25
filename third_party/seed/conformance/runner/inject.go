package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// preprocessAssertions scans assertions for mock/state queries with id=0
// and creates injected actions to query the CLI for the needed values.
// It mutates the assertions in-place to update their IDs.
func preprocessAssertions(spec *Spec) []SpecAction {
	var injectedActions []SpecAction
	nextID := 9001
	for i := range spec.Assertions {
		a := &spec.Assertions[i]
		if (a.Type == "mock" || a.Type == "state") && a.ID == 0 {
			var method string
			switch a.Path {
			case "callCount":
				method = "mock.callCount"
				a.Path = "count" // mock.callCount returns {"count": N}
			case "executorCallCount":
				method = "mock.executorCallCount"
				a.Path = "count" // mock.executorCallCount returns {"count": N}
			case "tokens":
				method = "state.tokens"
			case "cost":
				method = "state.cost"
			case "len":
				method = "state.len"
			case "sessionId":
				method = "state.sessionId"
			case "messages":
				method = "state.messages"
			case "checkpoints":
				method = "agent.checkpoints"
			}
			if method != "" {
				injectedActions = append(injectedActions, SpecAction{
					ID:     nextID,
					Method: method,
					Params: map[string]interface{}{},
				})
				a.ID = nextID
				nextID++
			}
		}
	}
	return injectedActions
}

// waitForRunResponse waits for the main run response to arrive before
// sending injected mock/state queries.
func waitForRunResponse(spec *Spec, result *RunSpecResult) {
	var runID int
	for _, act := range spec.Actions {
		if act.ID > runID {
			runID = act.ID
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		result.mu.Lock()
		_, ok := result.Response[runID]
		result.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, action := range spec.Actions {
		if action.Wait != nil {
			waitID := *action.Wait
			for time.Now().Before(deadline) {
				result.mu.Lock()
				_, ok := result.Response[waitID]
				result.mu.Unlock()
				if ok {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
	}
}

// sendInjectedActions writes injected actions to the CLI stdin and returns
// an error result if any action fails.
func sendInjectedActions(
	injectedActions []SpecAction,
	stdin io.Writer,
	verbose bool,
) (map[string]interface{}, bool) {
	for _, action := range injectedActions {
		req := map[string]interface{}{
			"id":     action.ID,
			"method": action.Method,
			"params": action.Params,
		}

		line, err := json.Marshal(req)
		if err != nil {
			return map[string]interface{}{
				"error": "failed to marshal injected action: " + err.Error(),
			}, false
		}

		if verbose {
			fmt.Fprintf(os.Stderr, "#   [stdin-injected]  %s\n", line)
		}

		if _, err := fmt.Fprintln(stdin, string(line)); err != nil {
			return map[string]interface{}{
				"error": "failed to send injected action: " + err.Error(),
			}, false
		}

		time.Sleep(10 * time.Millisecond)
	}
	return nil, true
}
