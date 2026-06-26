package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestIsParallelSafeBatchTool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		{name: "read_file is safe", toolName: "read_file", expected: true},
		{name: "fetch_url is safe", toolName: "fetch_url", expected: true},
		{name: "search_files is safe", toolName: "search_files", expected: true},
		{name: "write_file is not safe", toolName: "write_file", expected: false},
		{name: "shell_command is not safe", toolName: "shell_command", expected: false},
		{name: "edit_file is not safe", toolName: "edit_file", expected: false},
		{name: "empty name is not safe", toolName: "", expected: false},
		{name: "unknown tool is not safe", toolName: "browse_url", expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isParallelSafeBatchTool(tc.toolName); got != tc.expected {
				t.Errorf("isParallelSafeBatchTool(%q) = %v, expected %v", tc.toolName, got, tc.expected)
			}
		})
	}
}

func TestParallelWorkerLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		toolName  string
		batchSize int
		expected  int
	}{
		// fetch_url capped at 4
		{name: "fetch_url batch=2", toolName: "fetch_url", batchSize: 2, expected: 2},
		{name: "fetch_url batch=5", toolName: "fetch_url", batchSize: 5, expected: 4},
		{name: "fetch_url batch=20", toolName: "fetch_url", batchSize: 20, expected: 4},
		// search_files capped at 6
		{name: "search_files batch=3", toolName: "search_files", batchSize: 3, expected: 3},
		{name: "search_files batch=8", toolName: "search_files", batchSize: 8, expected: 6},
		{name: "search_files batch=50", toolName: "search_files", batchSize: 50, expected: 6},
		// default (read_file etc.) capped at 12
		{name: "read_file batch=5", toolName: "read_file", batchSize: 5, expected: 5},
		{name: "read_file batch=20", toolName: "read_file", batchSize: 20, expected: 12},
		{name: "unknown tool batch=10", toolName: "unknown", batchSize: 10, expected: 10},
		{name: "unknown tool batch=15", toolName: "unknown", batchSize: 15, expected: 12},
		// edge cases
		{name: "batch=0", toolName: "read_file", batchSize: 0, expected: 1},
		{name: "batch=1", toolName: "read_file", batchSize: 1, expected: 1},
		{name: "batch=-1", toolName: "fetch_url", batchSize: -1, expected: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parallelWorkerLimit(tc.toolName, tc.batchSize); got != tc.expected {
				t.Errorf("parallelWorkerLimit(%q, %d) = %d, expected %d", tc.toolName, tc.batchSize, got, tc.expected)
			}
		})
	}
}

func TestCanExecuteInParallel(t *testing.T) {
	tests := []struct {
		name      string
		toolCalls []api.ToolCall
		provider  string
		expected  bool
	}{
		{
			name: "single tool call - no parallel",
			toolCalls: []api.ToolCall{{ID: "c1", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: "{}"}}},
			expected: false,
		},
		{
			name: "same safe tool - parallel",
			toolCalls: []api.ToolCall{
				{ID: "c1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: `{"file_path":"a.txt"}`}},
				{ID: "c2", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: `{"file_path":"b.txt"}`}},
			},
			expected: true,
		},
		{
			name: "different tools - no parallel",
			toolCalls: []api.ToolCall{
				{ID: "c1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: "{}"}},
				{ID: "c2", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "write_file", Arguments: "{}"}},
			},
			expected: false,
		},
		{
			name: "same unsafe tool - no parallel",
			toolCalls: []api.ToolCall{
				{ID: "c1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "write_file", Arguments: "{}"}},
				{ID: "c2", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "write_file", Arguments: "{}"}},
			},
			expected: false,
		},
		{
			name:     "deepseek provider - no parallel",
			provider: "deepseek",
			toolCalls: []api.ToolCall{
				{ID: "c1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: "{}"}},
				{ID: "c2", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: "{}"}},
			},
			expected: false,
		},
		{
			name:     "minimax provider - no parallel",
			provider: "minimax",
			toolCalls: []api.ToolCall{
				{ID: "c1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: "{}"}},
				{ID: "c2", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: "{}"}},
			},
			expected: false,
		},
		{
			name: "same fetch_url - parallel",
			toolCalls: []api.ToolCall{
				{ID: "c1", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "fetch_url", Arguments: `{"url":"a.com"}`}},
				{ID: "c2", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "fetch_url", Arguments: `{"url":"b.com"}`}},
				{ID: "c3", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "fetch_url", Arguments: `{"url":"c.com"}`}},
			},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent := &Agent{
				output: NewAgentOutputManager(),
				state:  NewAgentStateManager(false),
			}
			if tc.provider != "" {
				agent.state.SetSessionProvider(api.ClientType(tc.provider))
			}
			te := &ToolExecutor{agent: agent}
			if got := te.canExecuteInParallel(tc.toolCalls); got != tc.expected {
				t.Errorf("canExecuteInParallel(%v) = %v, expected %v", tc.toolCalls, got, tc.expected)
			}
		})
	}
}
