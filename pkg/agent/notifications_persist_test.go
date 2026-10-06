package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pending wakeup notifications must survive ExportState → ImportState.
// This is the path that carries a background-task completion across agent
// teardown/restore (daemon idle eviction re-creates agents from the state
// snapshot); losing the queue meant the task finished but the conversation
// never heard about it.
func TestPendingNotifications_SurviveStateRoundTrip(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	a.QueueNotification(Notification{
		Content:   "Background session bg-foo completed.",
		SessionID: "bg-foo",
		Kind:      NotifShellBg,
	})
	a.QueueNotification(Notification{
		Content:   "Background session bg-bar timed out.",
		SessionID: "bg-bar",
		Kind:      NotifShellBgTimeout,
	})

	snapshot, err := a.ExportState()
	require.NoError(t, err)

	b := newTestAgent(t)
	defer b.Shutdown()
	require.NoError(t, b.ImportState(snapshot))

	got := b.DrainNotifications()
	require.Len(t, got, 2, "imported agent must carry the pending notifications")
	assert.Equal(t, "bg-foo", got[0].SessionID)
	assert.Equal(t, NotifShellBg, got[0].Kind)
	assert.Equal(t, "bg-bar", got[1].SessionID)
	assert.Equal(t, NotifShellBgTimeout, got[1].Kind)

	// The source agent's queue is untouched by export (snapshot, not drain).
	restored := a.DrainNotifications()
	assert.Len(t, restored, 2, "export must not drain the source queue")
}

// An exported snapshot with no pending notifications imports cleanly and
// leaves the queue empty.
func TestPendingNotifications_EmptyRoundTrip(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	snapshot, err := a.ExportState()
	require.NoError(t, err)

	b := newTestAgent(t)
	defer b.Shutdown()
	require.NoError(t, b.ImportState(snapshot))

	assert.Empty(t, b.DrainNotifications())
}

// TestCacheSavingsUnknown_SurvivesStateRoundTrip pins that the "savings
// unknown" flag survives ExportState → ImportState: without it, a restored
// session whose savings were undeterminable would render a misleading $0.
func TestCacheSavingsUnknown_SurvivesStateRoundTrip(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	a.state.SetCachedTokens(600)
	a.state.SetCachedCostSavings(0)
	a.state.SetCacheSavingsUnknown(true)

	snapshot, err := a.ExportState()
	require.NoError(t, err)

	b := newTestAgent(t)
	defer b.Shutdown()
	require.NoError(t, b.ImportState(snapshot))

	assert.True(t, b.GetCacheSavingsUnknown(), "restored agent must carry the unknown-savings flag")
	assert.Equal(t, "unknown", b.FormatCacheSavings())

	// A session with known savings restores as known.
	c := newTestAgent(t)
	defer c.Shutdown()
	c.state.SetCachedTokens(600)
	c.state.SetCachedCostSavings(0.0042)
	c.state.SetCacheSavingsUnknown(false)
	snap2, err := c.ExportState()
	require.NoError(t, err)
	d := newTestAgent(t)
	defer d.Shutdown()
	require.NoError(t, d.ImportState(snap2))
	assert.False(t, d.GetCacheSavingsUnknown())
	assert.Equal(t, "$0.004200", d.FormatCacheSavings())
}
