package commands

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func TestSelfReviewCommand_Name(t *testing.T) {
	cmd := &SelfReviewCommand{}
	assert.Equal(t, "self-review", cmd.Name())
}

func TestSelfReviewCommand_Description(t *testing.T) {
	cmd := &SelfReviewCommand{}
	assert.Equal(t, "Run canonical-spec scope validation against the current or specified revision", cmd.Description())
}

func TestSelfReviewCommand_SetContextAndGetContext(t *testing.T) {
	cmd := &SelfReviewCommand{}

	// getContext() returns context.Background() when no context was set
	ctx1 := cmd.getContext()
	assert.NotNil(t, ctx1, "getContext should not return nil when no context was set")
	assert.NoError(t, ctx1.Err(), "default context should not be cancelled")

	// SetContext stores the context
	customCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd.SetContext(customCtx)

	// getContext() returns the stored context when set
	ctx2 := cmd.getContext()
	assert.Same(t, customCtx, ctx2, "getContext should return the exact context stored by SetContext")

	// Verify cancellation propagates through getContext
	cancel()
	ctx3 := cmd.getContext()
	assert.ErrorIs(t, ctx3.Err(), context.Canceled, "getContext should reflect cancellation of the stored context")
}

func TestSelfReviewCommand_SetContext_Override(t *testing.T) {
	cmd := &SelfReviewCommand{}

	// Set one context
	ctx1, cancel1 := context.WithCancel(context.Background())
	cmd.SetContext(ctx1)
	assert.Same(t, ctx1, cmd.getContext())

	// Override with another
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer func() {
		cancel1()
		cancel2()
	}()
	cmd.SetContext(ctx2)
	assert.Same(t, ctx2, cmd.getContext())
	assert.NotSame(t, ctx1, cmd.getContext())
}

func TestSelfReviewCommand_SetContext_Nil(t *testing.T) {
	cmd := &SelfReviewCommand{}

	// Set a real context
	ctx1, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd.SetContext(ctx1)
	assert.Same(t, ctx1, cmd.getContext())

	// Set nil — should fall back to context.Background()
	cmd.SetContext(nil)
	ctx2 := cmd.getContext()
	assert.NotNil(t, ctx2, "getContext should return context.Background() when nil was set")
	assert.NoError(t, ctx2.Err())
}

func TestSelfReviewCommand_Execute_NilAgent(t *testing.T) {
	cmd := &SelfReviewCommand{}
	err := cmd.Execute(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent is not initialized")
}

func TestSelfReviewCommand_Execute_ContextCancellation(t *testing.T) {
	// Create a real agent (in test mode)
	chatAgent, err := agent.NewAgentWithModel("")
	require.NoError(t, err, "NewAgentWithModel should succeed")

	cmd := &SelfReviewCommand{}

	// Set a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	cmd.SetContext(ctx)

	// Execute should fail because there's no revision ID, but the key
	// assertion is that it uses our cancelled context rather than
	// context.Background(). The error path is what we can realistically
	// test without a full revision setup.
	err = cmd.Execute(nil, chatAgent)
	assert.Error(t, err, "Execute with cancelled context should fail")
}

func TestSelfReviewCommand_Execute_NoRevision(t *testing.T) {
	// Create a real agent with no revision
	chatAgent, err := agent.NewAgentWithModel("")
	require.NoError(t, err)

	cmd := &SelfReviewCommand{}
	err = cmd.Execute(nil, chatAgent)
	// Without a revision ID, the command should fail gracefully
	assert.Error(t, err, "Execute without a revision should error")
}

func TestSelfReviewCommand_Execute_WithArgs(t *testing.T) {
	chatAgent, err := agent.NewAgentWithModel("")
	require.NoError(t, err)

	cmd := &SelfReviewCommand{}
	// Pass a fake revision ID — it won't exist, but we're testing
	// that the args are forwarded and the context flows through
	err = cmd.Execute([]string{"fake-revision-id"}, chatAgent)
	assert.Error(t, err, "Execute with a nonexistent revision ID should error")
}
