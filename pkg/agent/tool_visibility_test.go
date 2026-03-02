package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestSetAndGetLastPreparedToolNames(t *testing.T) {
	a := &Agent{}
	a.SetLastPreparedToolNames([]api.Tool{
		{Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{Name: "read_file"}},
		{Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{Name: "write_file"}},
		{Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{Name: "read_file"}},
	})

	got := a.GetLastPreparedToolNames()
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d: %#v", len(got), got)
	}
	if got[0] != "read_file" || got[1] != "write_file" {
		t.Fatalf("unexpected tools: %#v", got)
	}
}
