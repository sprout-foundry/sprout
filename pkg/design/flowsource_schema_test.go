package design

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SP-140-9 §9b schema pins: the v1 document parses strictly, and `screen` is
// optional — a non-screen flow's steps (agent-turn) walk no surface, while a
// present screen must resolve against the tree's stems.
func TestFlowSourceSchemaScreenOptional(t *testing.T) {
	t.Run("non-screen-flow-parses", func(t *testing.T) {
		src, err := ParseFlowSource("design/flows/agent-turn.json", []byte(`{
			"name": "agent-turn",
			"steps": [
				{"id": "send", "label": "send", "next": "plan"},
				{"id": "plan", "label": "plan"}
			]
		}`))
		require.NoError(t, err)
		require.Len(t, src.Steps, 2)
		assert.Empty(t, src.Steps[0].Screen, "a non-screen step carries no screen")
	})

	t.Run("non-screen-flow-validates-clean", func(t *testing.T) {
		findings := ValidateFlowSource("design/flows/agent-turn.json", []byte(`{
			"name": "agent-turn",
			"steps": [{"id": "send", "label": "send"}]
		}`), nil)
		assert.Empty(t, findings, "no stems given and no screen named: clean, got %#v", findings)
	})

	t.Run("present-screen-must-resolve", func(t *testing.T) {
		findings := ValidateFlowSource("design/flows/agent-turn.json", []byte(`{
			"name": "agent-turn",
			"steps": [{"id": "send", "label": "send", "screen": "nowhere"}]
		}`), []string{"home"})
		require.Len(t, findings, 1)
		assert.Equal(t, ruleFlowSourceSchema, findings[0].Rule)
		assert.Equal(t, SeverityError, findings[0].Severity)
		assert.Contains(t, findings[0].Message, "nowhere")
	})

	t.Run("present-screen-resolving-is-clean", func(t *testing.T) {
		findings := ValidateFlowSource("design/flows/agent-turn.json", []byte(`{
			"name": "agent-turn",
			"steps": [{"id": "send", "label": "send", "screen": "home"}]
		}`), []string{"home"})
		assert.Empty(t, findings)
	})
}
