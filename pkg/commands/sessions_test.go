package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionsCommand_Name(t *testing.T) {
	cmd := &SessionsCommand{}
	assert.Equal(t, "sessions", cmd.Name())
}

func TestSessionsCommand_Description(t *testing.T) {
	cmd := &SessionsCommand{}
	desc := cmd.Description()
	assert.Contains(t, desc, "previous conversation")
	assert.Contains(t, desc, "session")
}
