//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"syscall/js"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

func llmJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"runQuestion": js.FuncOf(runQuestionFunc),
		"runCommit":   js.FuncOf(runCommitFunc),
		"runReview":   js.FuncOf(runReviewFunc),
	}
}

// runQuestionFunc runs a read-only Q&A agent against the workspace.
// Inputs:
//
//	args[0] (string)  — provider name
//	args[1] (string)  — model id (pass "" for the provider's default)
//	args[2] (string)  — the question to ask
//	args[3] (func?)   — onEvent(jsonString) callback for streamed UI events
//
// Returns a Promise resolving to:
//
//	{
//	  response: string,
//	  provider: string,
//	  model:    string,
//	  mode:     "question",
//	}
//
// The agent is configured with a read-only system prompt that instructs it
// to analyze code and explain behavior without modifying any files.
//
// Timeout: 5 minutes per call.
func runQuestionFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	model := argString(args, 1, "")
	question := argString(args, 2, "")

	var onEvent js.Value
	if len(args) > 3 && args[3].Type() == js.TypeFunction {
		onEvent = args[3]
	}

	return asPromiseWithTimeout(5*time.Minute, func(ctx context.Context) (interface{}, error) {
		if provider == "" {
			return nil, fmt.Errorf("provider is required (first arg)")
		}
		if question == "" {
			return nil, fmt.Errorf("question is required (third arg)")
		}

		client, err := factory.CreateProviderClient(api.ClientType(provider), model)
		if err != nil {
			return nil, fmt.Errorf("create client: %w", err)
		}

		configMgr, err := configuration.NewManagerSilent()
		if err != nil {
			return nil, fmt.Errorf("init configuration: %w", err)
		}

		ag, err := agent.NewAgentWithClient(client, api.ClientType(provider), configMgr)
		if err != nil {
			return nil, fmt.Errorf("init agent: %w", err)
		}

		ag.SetSystemPrompt("You are a code analysis assistant running in a browser WASM environment. You have access to the file system but MUST NOT modify any files. Answer questions about code, explain behavior, and provide analysis. Be concise and accurate. If you need to examine files, only read them — never write or edit anything. Provide thorough, well-structured explanations with code references where appropriate.")

		var unsubscribe func()
		if !onEvent.IsUndefined() && !onEvent.IsNull() {
			unsubscribe = wireAgentEventForwarding(ag, onEvent)
			defer unsubscribe()
		}

		response, err := ag.ProcessQuery(question)
		if err != nil {
			return nil, fmt.Errorf("process query: %w", err)
		}

		return map[string]interface{}{
			"response": response,
			"provider": provider,
			"model":    ag.GetModel(),
			"mode":     "question",
		}, nil
	})
}

// runCommitFunc generates a conventional commit message from staged diff
// content. Inputs:
//
//	args[0] (string)   — provider name
//	args[1] (string)   — model id (pass "" for the provider's default)
//	args[2] (string/object) — diff content (string or object with .diff/.content/.text)
//	args[3] (func?)    — onEvent(jsonString) callback for streamed UI events
//
// Returns a Promise resolving to:
//
//	{
//	  message:  string,
//	  provider: string,
//	  model:    string,
//	  mode:     "commit",
//	}
//
// Timeout: 2 minutes per call.
func runCommitFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	model := argString(args, 1, "")
	diffContent := argDiff(args, 2, "")

	var onEvent js.Value
	if len(args) > 3 && args[3].Type() == js.TypeFunction {
		onEvent = args[3]
	}

	return asPromiseWithTimeout(2*time.Minute, func(ctx context.Context) (interface{}, error) {
		if provider == "" {
			return nil, fmt.Errorf("provider is required (first arg)")
		}
		if diffContent == "" {
			return nil, fmt.Errorf("diff content is required (third arg)")
		}

		client, err := factory.CreateProviderClient(api.ClientType(provider), model)
		if err != nil {
			return nil, fmt.Errorf("create client: %w", err)
		}

		configMgr, err := configuration.NewManagerSilent()
		if err != nil {
			return nil, fmt.Errorf("init configuration: %w", err)
		}

		ag, err := agent.NewAgentWithClient(client, api.ClientType(provider), configMgr)
		if err != nil {
			return nil, fmt.Errorf("init agent: %w", err)
		}

		ag.SetSystemPrompt(`You are an expert at writing concise, conventional commit messages. Analyze the staged diff provided by the user and generate a proper commit message.

Rules:
1. Use conventional commit format: type(scope): description
2. Type must be one of: feat, fix, refactor, docs, style, test, chore, perf, ci, build, revert
3. Scope is optional but encouraged when changes are focused on a specific area
4. Title: Maximum 72 characters, imperative mood ("add" not "added")
5. Blank line after title
6. Body: Brief description of what changed and why (not how)
7. No markdown formatting, no code blocks, no filenames in title
8. Be concise but informative

IMPORTANT: Do NOT use any tools. Rely SOLELY on the diff content provided.`)

		var unsubscribe func()
		if !onEvent.IsUndefined() && !onEvent.IsNull() {
			unsubscribe = wireAgentEventForwarding(ag, onEvent)
			defer unsubscribe()
		}

		query := fmt.Sprintf("Generate a commit message for the following staged changes:\n\n%s", diffContent)
		response, err := ag.ProcessQuery(query)
		if err != nil {
			return nil, fmt.Errorf("process query: %w", err)
		}

		return map[string]interface{}{
			"message":  response,
			"provider": provider,
			"model":    ag.GetModel(),
			"mode":     "commit",
		}, nil
	})
}

// runReviewFunc performs a structured code review on diff content.
// Inputs:
//
//	args[0] (string)   — provider name
//	args[1] (string)   — model id (pass "" for the provider's default)
//	args[2] (string/object) — diff content (string or object with .diff/.content/.text)
//	args[3] (func?)    — onEvent(jsonString) callback for streamed UI events
//
// Returns a Promise resolving to:
//
//	{
//	  response: string,  // JSON-formatted review response
//	  provider: string,
//	  model:    string,
//	  mode:     "review",
//	}
//
// Timeout: 5 minutes per call.
func runReviewFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	model := argString(args, 1, "")
	diffContent := argDiff(args, 2, "")

	var onEvent js.Value
	if len(args) > 3 && args[3].Type() == js.TypeFunction {
		onEvent = args[3]
	}

	return asPromiseWithTimeout(5*time.Minute, func(ctx context.Context) (interface{}, error) {
		if provider == "" {
			return nil, fmt.Errorf("provider is required (first arg)")
		}
		if diffContent == "" {
			return nil, fmt.Errorf("diff content is required (third arg)")
		}

		client, err := factory.CreateProviderClient(api.ClientType(provider), model)
		if err != nil {
			return nil, fmt.Errorf("create client: %w", err)
		}

		configMgr, err := configuration.NewManagerSilent()
		if err != nil {
			return nil, fmt.Errorf("init configuration: %w", err)
		}

		ag, err := agent.NewAgentWithClient(client, api.ClientType(provider), configMgr)
		if err != nil {
			return nil, fmt.Errorf("init agent: %w", err)
		}

		ag.SetSystemPrompt(`You are an expert code reviewer. Analyze the diff provided by the user and produce a structured code review.

Review for:
1. **Bugs**: Logic errors, off-by-one errors, nil/null dereferences, race conditions
2. **Security**: Injection vulnerabilities, hardcoded secrets, insecure defaults, missing input validation
3. **Performance**: N+1 queries, unnecessary allocations, missing indexes, O(n²) where O(n) is possible
4. **Style**: Naming conventions, code organization, unnecessary complexity
5. **Completeness**: Missing error handling, missing tests for new logic, incomplete implementations

For each finding, classify as:
- MUST_FIX: Critical bugs, security vulnerabilities, data loss risks
- SHOULD_FIX: Notable issues that should be addressed before merging
- NIT: Style preferences, minor naming suggestions
- INFO: Observations, positive notes, or suggestions for future improvements

Respond in JSON format:
{
  "findings": [
    {"severity": "MUST_FIX|SHOULD_FIX|NIT|INFO", "file": "...", "line": N, "message": "..."}
  ],
  "summary": "Brief overall assessment of the changes",
  "approved": true/false
}

Set "approved" to true only if there are no MUST_FIX issues.
IMPORTANT: Do NOT use any tools. Rely SOLELY on the diff content provided.
Be thorough but fair. Focus on real issues, not hypothetical ones.`)

		var unsubscribe func()
		if !onEvent.IsUndefined() && !onEvent.IsNull() {
			unsubscribe = wireAgentEventForwarding(ag, onEvent)
			defer unsubscribe()
		}

		query := fmt.Sprintf("Review the following code changes:\n\n%s", diffContent)
		response, err := ag.ProcessQuery(query)
		if err != nil {
			return nil, fmt.Errorf("process query: %w", err)
		}

		return map[string]interface{}{
			"response": response,
			"provider": provider,
			"model":    ag.GetModel(),
			"mode":     "review",
		}, nil
	})
}

// argDiff extracts a diff/content string from a positional argument,
// supporting both a raw string and an object with .diff, .content, or .text
// properties. Falls back to defaultVal when the slot is missing, empty, or
// contains none of the expected shapes. Empty strings (whether passed directly
// or via an object property) are treated as absent and trigger the fallback
// to the next property or defaultVal.
func argDiff(args []js.Value, idx int, defaultVal string) string {
	if idx >= len(args) {
		return defaultVal
	}
	v := args[idx]
	if v.Type() == js.TypeString {
		s := v.String()
		if s != "" {
			return s
		}
		return defaultVal
	}
	if v.Type() == js.TypeObject {
		for _, key := range []string{"diff", "content", "text"} {
			prop := v.Get(key)
			if prop.Type() == js.TypeString && prop.String() != "" {
				return prop.String()
			}
		}
	}
	return defaultVal
}
