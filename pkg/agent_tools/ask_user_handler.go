package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
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
			{Name: "options", Type: "array", Required: false, Description: "Selectable choices: {label, value?, description?}",
				Items: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label":       map[string]any{"type": "string", "description": "Human-readable choice label (required)"},
						"value":       map[string]any{"type": "string", "description": "Optional machine value returned on selection (falls back to label)"},
						"description": map[string]any{"type": "string", "description": "Optional short description of the choice"},
					},
					"required": []any{"label"},
				}},
			{Name: "multi_select", Type: "boolean", Required: false, Description: "Allow multiple selections (default false)"},
			{Name: "default", Type: "string", Required: false, Description: "Default response when user submits empty"},
			{Name: "sensitive", Type: "boolean", Required: false, Description: "Marks the response as a credential: the UI renders a masked input and the value is written straight to the credential store. The model receives only a confirmation, never the value. Requires credential_key; not compatible with options."},
			{Name: "credential_key", Type: "string", Required: false, Description: "Credential-store key for sensitive prompts (e.g. mcp/figma/FIGMA_TOKEN). Required when sensitive=true."},
		},
	}
}

func (h *askUserHandler) Validate(args map[string]any) error {
	if _, err := extractString(args, "question"); err != nil {
		return err
	}
	// A sensitive request must name its credential target, or the response
	// would have nowhere safe to go.
	if sensitive, _ := args["sensitive"].(bool); sensitive {
		key, _ := args["credential_key"].(string)
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("sensitive=true requires credential_key (the credential-store key to write, e.g. mcp/figma/FIGMA_TOKEN)")
		}
		if !strings.HasPrefix(key, "mcp/") {
			return fmt.Errorf("credential_key must start with mcp/ (e.g. mcp/figma/FIGMA_TOKEN)")
		}
	}
	return nil
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
		response, err = AskUser(ctx, req)
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
	// CLI path: the sensitive value was read from the terminal (no manager
	// in the loop to divert it), so store it here and return only the
	// confirmation. WebUI path: RespondToAskUser already diverted the value
	// and `response` IS the confirmation.
	if req.Sensitive {
		return h.storeSensitiveCLI(env, req, response)
	}
	return ToolResult{Output: response}, nil
}

// storeSensitiveCLI persists a sensitive answer collected on the CLI path
// and returns the masked confirmation. An empty answer stores nothing.
func (h *askUserHandler) storeSensitiveCLI(env ToolEnv, req AskUserRequest, response string) (ToolResult, error) {
	if strings.TrimSpace(response) == "" {
		return ToolResult{Output: "ask_user: the user submitted an empty credential; nothing was stored. Ask again or choose another path."}, nil
	}
	if err := credentials.SetToActiveBackend(req.CredentialKey, response); err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("ask_user: storing the credential failed (%v). The value was NOT saved; ask the user to retry or use Settings.", err),
			IsError: true,
		}, nil
	}
	return ToolResult{
		Output: fmt.Sprintf("Credential stored securely under %s. It is not visible in this conversation. Continue: restart or refresh the MCP server so it picks up the credential, then verify with mcp_refresh (operation: list).", req.CredentialKey),
	}, nil
}

func (h *askUserHandler) Aliases() []string { return nil }

// Timeout must match DefaultAskUserTimeout (the inner manager's deadline) so
// the seed ToolRegistry's wrapper doesn't cancel the caller's context before
// the inner manager's own timeout, which would silently drop the user's
// in-progress answer while the prompt UI stays open.
func (h *askUserHandler) Timeout() time.Duration { return DefaultAskUserTimeout }

func (h *askUserHandler) MaxResultSize() int    { return 0 }
func (h *askUserHandler) SafeForParallel() bool { return false }
func (h *askUserHandler) Interactive() bool     { return true }

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
	if s, ok := args["sensitive"].(bool); ok {
		req.Sensitive = s
	}
	if k, ok := args["credential_key"].(string); ok {
		req.CredentialKey = k
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
