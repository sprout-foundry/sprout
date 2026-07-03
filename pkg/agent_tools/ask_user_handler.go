package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type askUserHandler struct{}

func (h *askUserHandler) Name() string { return "ask_user" }

func (h *askUserHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "ask_user",
		Description: "Ask the user a question and wait for their response. Use this when you need clarification, a decision, or any input that cannot be determined from context alone.\n\n**Pass `options` whenever the answer is one of a small set of choices** (Yes/No, A/B/C, file paths to confirm). The UI renders them as buttons; the CLI renders a numbered list. The returned value is the option's `value` (falling back to `label`), so prefer machine-friendly `value` strings.\n\nSet `multi_select: true` for checkbox-style selection (response is comma-joined values). Set `default` to the option `value` (or freeform string) that should be pre-selected.",
		Required:    []string{"question"},
		Parameters: []ParameterDef{
			{Name: "question", Type: "string", Required: true, Description: "The question to ask the user. Markdown is supported in the WebUI; the CLI renders plain text."},
			{Name: "header", Type: "string", Required: false, Description: "Short label (≤ 40 chars) shown above the question — useful for categorizing the prompt (e.g., \"Auth method\", \"Approach\", \"Confirm delete\")."},
			{Name: "options", Type: "array", Required: false, Description: "Optional array of selectable choices. Each entry is {label, value?, description?}. When omitted the user types a freeform response."},
			{Name: "multi_select", Type: "boolean", Required: false, Description: "When true, the user may pick multiple options. Response is a comma-joined list of selected values. Default false."},
			{Name: "default", Type: "string", Required: false, Description: "Default response. Should match an option's `value` (or `label`) when `options` is set; otherwise it's used as the freeform default when the user submits empty input."},
		},
	}
}

func (h *askUserHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "question")
	return err
}

func (h *askUserHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()
	}

	req, err := parseAskUserArgs(args)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("ask_user failed: %v", err), IsError: true}, nil
	}

	var response string
	if env.AskUser != nil {
		response, err = env.AskUser.Ask(ctx, req)
	} else {
		response, err = AskUser(req)
	}
	if err != nil {
		if errors.Is(err, ErrAskUserNoChannel) {
			return ToolResult{
				Output:  "ask_user: no interactive input channel is available — neither a WebUI client nor an interactive terminal is connected. Make a best-effort decision based on the existing context, or report that you cannot proceed without user input.",
				IsError: true,
			}, nil
		}
		return ToolResult{Output: fmt.Sprintf("ask_user failed: %v", err), IsError: true}, nil
	}
	return ToolResult{Output: response}, nil
}

func (h *askUserHandler) Aliases() []string         { return nil }
func (h *askUserHandler) Timeout() time.Duration    { return 0 }
func (h *askUserHandler) MaxResultSize() int        { return 0 }
func (h *askUserHandler) SafeForParallel() bool     { return false }
func (h *askUserHandler) Interactive() bool         { return true }

// parseAskUserArgs lifts a raw JSON-decoded args map into an AskUserRequest.
// Tolerant of LLM imperfection: accepts options as either []map or []string.
func parseAskUserArgs(args map[string]any) (AskUserRequest, error) {
	question, err := extractString(args, "question")
	if err != nil {
		return AskUserRequest{}, err
	}
	req := AskUserRequest{Question: question}
	if h, ok := args["header"].(string); ok {
		req.Header = h
	}
	if d, ok := args["default"].(string); ok {
		req.Default = d
	}
	switch m := args["multi_select"].(type) {
	case bool:
		req.MultiSelect = m
	case string:
		req.MultiSelect = m == "true"
	}
	if raw, ok := args["options"]; ok {
		req.Options = coerceOptionList(raw)
	}
	return req, nil
}

func coerceOptionList(raw any) []AskUserOption {
	switch v := raw.(type) {
	case []any:
		out := make([]AskUserOption, 0, len(v))
		for _, entry := range v {
			switch e := entry.(type) {
			case string:
				if s := e; s != "" {
					out = append(out, AskUserOption{Label: s})
				}
			case map[string]any:
				opt := AskUserOption{}
				if s, ok := e["label"].(string); ok {
					opt.Label = s
				}
				if s, ok := e["value"].(string); ok {
					opt.Value = s
				}
				if s, ok := e["description"].(string); ok {
					opt.Description = s
				}
				if opt.Label != "" {
					out = append(out, opt)
				}
			}
		}
		return out
	case []string:
		out := make([]AskUserOption, 0, len(v))
		for _, s := range v {
			if s != "" {
				out = append(out, AskUserOption{Label: s})
			}
		}
		return out
	}
	return nil
}
