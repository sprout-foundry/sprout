package tools

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type askUserHandler struct{}

func (h *askUserHandler) Name() string { return "ask_user" }

func (h *askUserHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "ask_user",
		Description: "Ask the user a question and wait for their response. Use options for small choice sets (renders as buttons in WebUI). Set multi_select for checkboxes.",
		Required:    []string{"question"},
		Parameters: []ParameterDef{
			{Name: "question", Type: "string", Required: true, Description: "Question to ask (supports Markdown)"},
			{Name: "header", Type: "string", Required: false, Description: "Short label (≤40 chars) for categorizing the prompt"},
			{Name: "options", Type: "array", Required: false, Description: "Selectable choices: {label, value?, description?}"},
			{Name: "multi_select", Type: "boolean", Required: false, Description: "Allow multiple selections (default false)"},
			{Name: "default", Type: "string", Required: false, Description: "Default response when user submits empty"},
		},
	}
}

func (h *askUserHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "question")
	return err
}

func (h *askUserHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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
