package mcp

import (
	"encoding/json"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Error Code Constants
// ---------------------------------------------------------------------------

func TestErrorCodes(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		expected int
	}{
		{"Parse", ErrorCodeParse, -32700},
		{"InvalidRequest", ErrorCodeInvalidRequest, -32600},
		{"MethodNotFound", ErrorCodeMethodNotFound, -32601},
		{"InvalidParams", ErrorCodeInvalidParams, -32602},
		{"InternalError", ErrorCodeInternalError, -32603},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.code != tc.expected {
				t.Errorf("%s = %d, want %d", tc.name, tc.code, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPServerConfig MarshalJSON edge cases
// ---------------------------------------------------------------------------

func TestMCPServerConfig_MarshalJSON_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		config   MCPServerConfig
		wantJSON string
	}{
		{
			name: "zero value config",
			config: MCPServerConfig{
				Name: "",
			},
			wantJSON: `{"name":"","auto_start":false,"max_restarts":0}`,
		},
		{
			name: "only name set",
			config: MCPServerConfig{
				Name: "my-server",
			},
			wantJSON: `{"name":"my-server","auto_start":false,"max_restarts":0}`,
		},
		{
			name: "nil credentials marshaled as null",
			config: MCPServerConfig{
				Name:        "test",
				Credentials: nil,
			},
			wantJSON: `{"name":"test","auto_start":false,"max_restarts":0}`,
		},
		{
			name: "timeout zero",
			config: MCPServerConfig{
				Name:    "test",
				Timeout: 0,
			},
			wantJSON: `{"name":"test","auto_start":false,"max_restarts":0}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.config)
			if err != nil {
				t.Fatalf("MarshalJSON failed: %v", err)
			}
			got := string(data)
			if got != tc.wantJSON {
				t.Errorf("MarshalJSON mismatch:\n  got:  %s\n  want: %s", got, tc.wantJSON)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPServerConfig UnmarshalJSON with credentials
// ---------------------------------------------------------------------------

func TestMCPServerConfig_UnmarshalJSON_WithCredentials(t *testing.T) {
	tests := []struct {
		name   string
		json   string
		expect MCPServerConfig
	}{
		{
			name: "with credentials",
			json: `{
				"name": "my-server",
				"type": "stdio",
				"command": "npx",
				"args": ["-y", "server"],
				"credentials": {
					"API_KEY": "{{credential:mcp/my-server/API_KEY}}",
					"AUTH_TOKEN": "{{credential:mcp/my-server/AUTH_TOKEN}}"
				},
				"timeout": "30s"
			}`,
			expect: MCPServerConfig{
				Name:    "my-server",
				Type:    "stdio",
				Command: "npx",
				Args:    []string{"-y", "server"},
				Credentials: map[string]string{
					"API_KEY":    "{{credential:mcp/my-server/API_KEY}}",
					"AUTH_TOKEN": "{{credential:mcp/my-server/AUTH_TOKEN}}",
				},
				Timeout: 30 * time.Second,
			},
		},
		{
			name: "with env and credentials",
			json: `{
				"name": "http-server",
				"type": "http",
				"url": "http://localhost:3000",
				"env": {"PATH": "/usr/bin"},
				"credentials": {"BEARER": "token-xyz"},
				"timeout": "60s"
			}`,
			expect: MCPServerConfig{
				Name:        "http-server",
				Type:        "http",
				URL:         "http://localhost:3000",
				Env:         map[string]string{"PATH": "/usr/bin"},
				Credentials: map[string]string{"BEARER": "token-xyz"},
				Timeout:     60 * time.Second,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var config MCPServerConfig
			if err := json.Unmarshal([]byte(tc.json), &config); err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}
			assertMCPServerConfigEqual(t, config, tc.expect)
		})
	}
}

// ---------------------------------------------------------------------------
// MCPServerConfig Marshal -> Unmarshal round-trip with credentials
// ---------------------------------------------------------------------------

func TestMCPServerConfig_MarshalUnmarshal_RoundTrip_WithCredentials(t *testing.T) {
	tests := []struct {
		name   string
		config MCPServerConfig
	}{
		{
			name: "full config with credentials",
			config: MCPServerConfig{
				Name:        "server",
				Type:        "stdio",
				Command:     "npx",
				Args:        []string{"-y", "server"},
				WorkingDir:  "/workspace",
				Timeout:     45 * time.Second,
				AutoStart:   true,
				MaxRestarts: 3,
				Env: map[string]string{
					"PATH": "/usr/bin",
				},
				Credentials: map[string]string{
					"API_KEY": "{{credential:mcp/server/API_KEY}}",
				},
			},
		},
		{
			name: "config with only credentials",
			config: MCPServerConfig{
				Name:        "creds-only",
				Credentials: map[string]string{"TOKEN": "val"},
				Timeout:     30 * time.Second,
			},
		},
		{
			name: "config with nil credentials",
			config: MCPServerConfig{
				Name:        "no-creds",
				Timeout:     15 * time.Second,
				Credentials: nil,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.config)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var out MCPServerConfig
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			assertMCPServerConfigEqual(t, out, tc.config)
		})
	}
}

// ---------------------------------------------------------------------------
// MCPError JSON marshal and unmarshal
// ---------------------------------------------------------------------------

func TestMCPError_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		err      MCPError
		wantJSON string
	}{
		{
			name: "with data",
			err: MCPError{
				Code:    -32601,
				Message: "Method not found",
				Data:    "details",
			},
			wantJSON: `{"code":-32601,"message":"Method not found","data":"details"}`,
		},
		{
			name:     "without data (nil)",
			err:      MCPError{Code: -32700, Message: "Parse error"},
			wantJSON: `{"code":-32700,"message":"Parse error"}`,
		},
		{
			name:     "zero values",
			err:      MCPError{},
			wantJSON: `{"code":0,"message":""}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.err)
			if err != nil {
				t.Fatalf("MarshalJSON failed: %v", err)
			}
			got := string(data)
			if got != tc.wantJSON {
				t.Errorf("MarshalJSON mismatch:\n  got:  %s\n  want: %s", got, tc.wantJSON)
			}
		})
	}
}

func TestMCPError_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name   string
		json   string
		expect MCPError
	}{
		{
			name: "with data",
			json: `{"code":-32601,"message":"Method not found","data":"details"}`,
			expect: MCPError{
				Code:    -32601,
				Message: "Method not found",
				Data:    "details",
			},
		},
		{
			name: "without data field",
			json: `{"code":-32700,"message":"Parse error"}`,
			expect: MCPError{
				Code:    -32700,
				Message: "Parse error",
			},
		},
		{
			name: "data as object",
			json: `{"code":-32602,"message":"Invalid params","data":{"key":"value"}}`,
			expect: MCPError{
				Code:    -32602,
				Message: "Invalid params",
				Data:    map[string]interface{}{"key": "value"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var e MCPError
			if err := json.Unmarshal([]byte(tc.json), &e); err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}
			if e.Code != tc.expect.Code {
				t.Errorf("Code = %d, want %d", e.Code, tc.expect.Code)
			}
			if e.Message != tc.expect.Message {
				t.Errorf("Message = %q, want %q", e.Message, tc.expect.Message)
			}
			// Data comparison
			if tc.expect.Data == nil && e.Data != nil {
				t.Errorf("Data = %v, want nil", e.Data)
			} else if tc.expect.Data != nil {
				dataJSON, _ := json.Marshal(tc.expect.Data)
				gotJSON, _ := json.Marshal(e.Data)
				if string(dataJSON) != string(gotJSON) {
					t.Errorf("Data mismatch:\n  got:  %s\n  want: %s", string(gotJSON), string(dataJSON))
				}
			}
		})
	}
}

func TestMCPError_Error_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		err      *MCPError
		expected string
	}{
		{
			name:     "zero code and empty message",
			err:      &MCPError{Code: 0, Message: ""},
			expected: "MCP error 0: ",
		},
		{
			name:     "positive code",
			err:      &MCPError{Code: 123, Message: "custom"},
			expected: "MCP error 123: custom",
		},
		{
			name:     "negative code (parse)",
			err:      &MCPError{Code: ErrorCodeParse, Message: "Parse error"},
			expected: "MCP error -32700: Parse error",
		},
		{
			name:     "negative code (internal)",
			err:      &MCPError{Code: ErrorCodeInternalError, Message: "Internal error"},
			expected: "MCP error -32603: Internal error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.err.Error()
			if got != tc.expected {
				t.Errorf("Error() = %q, want %q", got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPMessage JSON round-trip
// ---------------------------------------------------------------------------

func TestMCPMessage_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		msg  MCPMessage
	}{
		{
			name: "request with string id",
			msg: MCPMessage{
				JSONRPC: "2.0",
				ID:      "abc-123",
				Method:  "tools/call",
				Params:  map[string]interface{}{"name": "mytool"},
			},
		},
		{
			name: "request with int id",
			msg: MCPMessage{
				JSONRPC: "2.0",
				ID:      42,
				Method:  "initialize",
				Params:  map[string]interface{}{"protocolVersion": "2024-11-05"},
			},
		},
		{
			name: "result message",
			msg: MCPMessage{
				JSONRPC: "2.0",
				ID:      1,
				Result:  map[string]interface{}{"tools": []interface{}{}},
			},
		},
		{
			name: "error message",
			msg: MCPMessage{
				JSONRPC: "2.0",
				ID:      2,
				Error: &MCPError{
					Code:    ErrorCodeMethodNotFound,
					Message: "Method not found",
				},
			},
		},
		{
			name: "minimal message (zero value)",
			msg:  MCPMessage{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.msg)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var out MCPMessage
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if out.JSONRPC != tc.msg.JSONRPC {
				t.Errorf("JSONRPC = %q, want %q", out.JSONRPC, tc.msg.JSONRPC)
			}
			if out.Method != tc.msg.Method {
				t.Errorf("Method = %q, want %q", out.Method, tc.msg.Method)
			}
			// ID comparison (may be string or int)
			if tc.msg.ID != nil {
				idJSON, _ := json.Marshal(tc.msg.ID)
				gotJSON, _ := json.Marshal(out.ID)
				if string(idJSON) != string(gotJSON) {
					t.Errorf("ID mismatch: got %s, want %s", string(gotJSON), string(idJSON))
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPTool JSON round-trip
// ---------------------------------------------------------------------------

func TestMCPTool_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		tool MCPTool
	}{
		{
			name: "full tool",
			tool: MCPTool{
				Name:        "read_file",
				Description: "Reads a file",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{"type": "string"},
					},
				},
				ServerName: "filesystem",
			},
		},
		{
			name: "minimal tool",
			tool: MCPTool{
				Name: "ping",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.tool)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var out MCPTool
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if out.Name != tc.tool.Name {
				t.Errorf("Name = %q, want %q", out.Name, tc.tool.Name)
			}
			if out.Description != tc.tool.Description {
				t.Errorf("Description = %q, want %q", out.Description, tc.tool.Description)
			}
			if out.ServerName != tc.tool.ServerName {
				t.Errorf("ServerName = %q, want %q", out.ServerName, tc.tool.ServerName)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPResource JSON round-trip
// ---------------------------------------------------------------------------

func TestMCPResource_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		resource MCPResource
	}{
		{
			name: "full resource",
			resource: MCPResource{
				URI:         "file:///workspace/main.go",
				Name:        "main.go",
				Description: "Main Go source file",
				MimeType:    "text/x-go",
				ServerName:  "filesystem",
			},
		},
		{
			name: "minimal resource",
			resource: MCPResource{
				URI:  "file:///test.txt",
				Name: "test.txt",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.resource)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var out MCPResource
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if out.URI != tc.resource.URI {
				t.Errorf("URI = %q, want %q", out.URI, tc.resource.URI)
			}
			if out.Name != tc.resource.Name {
				t.Errorf("Name = %q, want %q", out.Name, tc.resource.Name)
			}
			if out.Description != tc.resource.Description {
				t.Errorf("Description = %q, want %q", out.Description, tc.resource.Description)
			}
			if out.MimeType != tc.resource.MimeType {
				t.Errorf("MimeType = %q, want %q", out.MimeType, tc.resource.MimeType)
			}
			if out.ServerName != tc.resource.ServerName {
				t.Errorf("ServerName = %q, want %q", out.ServerName, tc.resource.ServerName)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPPrompt and MCPPromptArgument JSON round-trip
// ---------------------------------------------------------------------------

func TestMCPPrompt_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		prompt MCPPrompt
	}{
		{
			name: "prompt with arguments",
			prompt: MCPPrompt{
				Name:        "summarize",
				Description: "Summarize a document",
				Arguments: []MCPPromptArgument{
					{Name: "url", Description: "URL to summarize", Required: true},
					{Name: "length", Description: "Max length", Required: false},
				},
				ServerName: "docs",
			},
		},
		{
			name: "prompt without arguments",
			prompt: MCPPrompt{
				Name:       "greet",
				ServerName: "bot",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.prompt)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var out MCPPrompt
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if out.Name != tc.prompt.Name {
				t.Errorf("Name = %q, want %q", out.Name, tc.prompt.Name)
			}
			if out.Description != tc.prompt.Description {
				t.Errorf("Description = %q, want %q", out.Description, tc.prompt.Description)
			}
			if out.ServerName != tc.prompt.ServerName {
				t.Errorf("ServerName = %q, want %q", out.ServerName, tc.prompt.ServerName)
			}
			if len(out.Arguments) != len(tc.prompt.Arguments) {
				t.Fatalf("Arguments length = %d, want %d", len(out.Arguments), len(tc.prompt.Arguments))
			}
			for i := range tc.prompt.Arguments {
				a := tc.prompt.Arguments[i]
				g := out.Arguments[i]
				if g.Name != a.Name {
					t.Errorf("Arg[%d] Name = %q, want %q", i, g.Name, a.Name)
				}
				if g.Description != a.Description {
					t.Errorf("Arg[%d] Description = %q, want %q", i, g.Description, a.Description)
				}
				if g.Required != a.Required {
					t.Errorf("Arg[%d] Required = %v, want %v", i, g.Required, a.Required)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPPromptArgument JSON round-trip (standalone)
// ---------------------------------------------------------------------------

func TestMCPPromptArgument_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		arg      MCPPromptArgument
		required bool
	}{
		{
			name: "required argument",
			arg:  MCPPromptArgument{Name: "name", Description: "A name", Required: true},
		},
		{
			name: "optional argument",
			arg:  MCPPromptArgument{Name: "color", Required: false},
		},
		{
			name: "zero value",
			arg:  MCPPromptArgument{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.arg)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var out MCPPromptArgument
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if out.Name != tc.arg.Name {
				t.Errorf("Name = %q, want %q", out.Name, tc.arg.Name)
			}
			if out.Description != tc.arg.Description {
				t.Errorf("Description = %q, want %q", out.Description, tc.arg.Description)
			}
			if out.Required != tc.arg.Required {
				t.Errorf("Required = %v, want %v", out.Required, tc.arg.Required)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPToolCallRequest JSON round-trip
// ---------------------------------------------------------------------------

func TestMCPToolCallRequest_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		request MCPToolCallRequest
	}{
		{
			name: "with arguments",
			request: MCPToolCallRequest{
				Name: "read_file",
				Arguments: map[string]interface{}{
					"path": "/workspace/main.go",
				},
			},
		},
		{
			name:    "without arguments",
			request: MCPToolCallRequest{Name: "ping"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.request)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var out MCPToolCallRequest
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if out.Name != tc.request.Name {
				t.Errorf("Name = %q, want %q", out.Name, tc.request.Name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPToolCallResult JSON round-trip
// ---------------------------------------------------------------------------

func TestMCPToolCallResult_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name   string
		result MCPToolCallResult
	}{
		{
			name: "success result",
			result: MCPToolCallResult{
				Content: []MCPContent{
					{Type: "text", Text: "Hello world"},
				},
				IsError: false,
			},
		},
		{
			name: "error result",
			result: MCPToolCallResult{
				Content: []MCPContent{
					{Type: "text", Text: "Something went wrong"},
				},
				IsError: true,
			},
		},
		{
			name:   "empty result",
			result: MCPToolCallResult{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.result)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var out MCPToolCallResult
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if out.IsError != tc.result.IsError {
				t.Errorf("IsError = %v, want %v", out.IsError, tc.result.IsError)
			}
			if len(out.Content) != len(tc.result.Content) {
				t.Fatalf("Content length = %d, want %d", len(out.Content), len(tc.result.Content))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPContent JSON round-trip
// ---------------------------------------------------------------------------

func TestMCPContent_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		content MCPContent
	}{
		{
			name:    "text content",
			content: MCPContent{Type: "text", Text: "Hello"},
		},
		{
			name:    "image content",
			content: MCPContent{Type: "image", Data: "base64data", MimeType: "image/png"},
		},
		{
			name: "content with annotations",
			content: MCPContent{
				Type: "text",
				Text: "Noted",
				Annotations: map[string]interface{}{
					"audience": "user",
					"priority": 0.8,
				},
			},
		},
		{
			name:    "zero value",
			content: MCPContent{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.content)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			var out MCPContent
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			if out.Type != tc.content.Type {
				t.Errorf("Type = %q, want %q", out.Type, tc.content.Type)
			}
			if out.Text != tc.content.Text {
				t.Errorf("Text = %q, want %q", out.Text, tc.content.Text)
			}
			if out.Data != tc.content.Data {
				t.Errorf("Data = %q, want %q", out.Data, tc.content.Data)
			}
			if out.MimeType != tc.content.MimeType {
				t.Errorf("MimeType = %q, want %q", out.MimeType, tc.content.MimeType)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helper: assert MCPServerConfig fields match
// ---------------------------------------------------------------------------

func assertMCPServerConfigEqual(t *testing.T, got, want MCPServerConfig) {
	t.Helper()
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.Type != want.Type {
		t.Errorf("Type = %q, want %q", got.Type, want.Type)
	}
	if got.Command != want.Command {
		t.Errorf("Command = %q, want %q", got.Command, want.Command)
	}
	if len(got.Args) != len(want.Args) {
		t.Fatalf("Args length = %d, want %d", len(got.Args), len(want.Args))
	}
	for i := range want.Args {
		if got.Args[i] != want.Args[i] {
			t.Errorf("Args[%d] = %q, want %q", i, got.Args[i], want.Args[i])
		}
	}
	if got.URL != want.URL {
		t.Errorf("URL = %q, want %q", got.URL, want.URL)
	}
	if got.WorkingDir != want.WorkingDir {
		t.Errorf("WorkingDir = %q, want %q", got.WorkingDir, want.WorkingDir)
	}
	if got.Timeout != want.Timeout {
		t.Errorf("Timeout = %v, want %v", got.Timeout, want.Timeout)
	}
	if got.AutoStart != want.AutoStart {
		t.Errorf("AutoStart = %v, want %v", got.AutoStart, want.AutoStart)
	}
	if got.MaxRestarts != want.MaxRestarts {
		t.Errorf("MaxRestarts = %d, want %d", got.MaxRestarts, want.MaxRestarts)
	}
	// Env comparison
	if len(got.Env) != len(want.Env) {
		t.Fatalf("Env length = %d, want %d", len(got.Env), len(want.Env))
	}
	for k, v := range want.Env {
		if got.Env[k] != v {
			t.Errorf("Env[%q] = %q, want %q", k, got.Env[k], v)
		}
	}
	// Credentials comparison
	if len(got.Credentials) != len(want.Credentials) {
		t.Fatalf("Credentials length = %d, want %d", len(got.Credentials), len(want.Credentials))
	}
	for k, v := range want.Credentials {
		if got.Credentials[k] != v {
			t.Errorf("Credentials[%q] = %q, want %q", k, got.Credentials[k], v)
		}
	}
}
