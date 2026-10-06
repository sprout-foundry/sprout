package console

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func keyedList(opts SelectListOptions) *SelectList {
	opts.Items = []SelectItem{
		{Label: "Approve once", Value: "approve_once", Key: 'y'},
		{Label: "Deny", Value: "deny", Key: 'n'},
		{Label: "Elevate", Value: "elevate"},
	}
	return NewSelectList(opts)
}

func press(s *SelectList, b byte) (bool, string, bool) {
	return s.processKey(b, 1, []byte{b})
}

func TestSelectList_ShortcutPicksAndConfirms(t *testing.T) {
	s := keyedList(SelectListOptions{})
	done, val, ok := press(s, 'N')
	require.True(t, done)
	require.True(t, ok)
	require.Equal(t, "deny", val, "shortcuts ignore case")

	done, _, _ = press(keyedList(SelectListOptions{}), 'e')
	require.False(t, done, "an item without a key has no shortcut")
}

func TestSelectList_ShortcutsOffWhenSearchable(t *testing.T) {
	s := keyedList(SelectListOptions{Searchable: true})
	done, _, _ := press(s, 'y')
	require.False(t, done)
	require.Equal(t, "y", s.filter, "in a searchable list typing filters")
}

func TestSelectList_ArmDelayHoldsBackConfirmingKeys(t *testing.T) {
	s := keyedList(SelectListOptions{ArmDelay: time.Hour})
	s.armedAt = time.Now().Add(time.Hour)
	done, _, _ := press(s, 'y')
	require.False(t, done, "a typed-ahead shortcut must not approve")
	done, _, _ = press(s, '\r')
	require.False(t, done, "a typed-ahead Enter must not approve")
	done, _, ok := press(s, 0x03)
	require.True(t, done)
	require.False(t, ok, "cancelling is never held back")

	s.armedAt = time.Now()
	done, val, _ := press(s, 'y')
	require.True(t, done)
	require.Equal(t, "approve_once", val)
}

func TestSelectList_KeyedLabelsLineUp(t *testing.T) {
	s := keyedList(SelectListOptions{})
	require.Equal(t, "[y] Approve once", s.keyedLabel(s.opts.Items[0]))
	require.Equal(t, "    Elevate", s.keyedLabel(s.opts.Items[2]))
	plain := NewSelectList(SelectListOptions{Items: []SelectItem{{Label: "a"}}})
	require.Equal(t, "a", plain.keyedLabel(plain.opts.Items[0]))
}

func TestApprovalFooterListsTheKeys(t *testing.T) {
	got := approvalFooter([]SelectItem{{Key: 'y'}, {Key: 'n'}, {}})
	require.True(t, strings.HasPrefix(got, "y/n or "), got)
}

func TestStatusFooter_HintOverrideReplacesComposerHints(t *testing.T) {
	var buf strings.Builder
	f := newShiftTestFooter(&buf, 24)
	f.showKeymapHint = true
	f.composerMode = ComposerSteer

	require.True(t, f.SetHintOverride("y/n or ↑/↓ + Enter · Esc denies"))
	f.drawFullLocked()
	require.Contains(t, buf.String(), "y/n or ↑/↓ + Enter")
	require.NotContains(t, buf.String(), "Enter steer")

	buf.Reset()
	f.SetHintOverride("")
	f.drawFullLocked()
	require.Contains(t, buf.String(), "Enter steer")
}

func TestSelectList_FrameOmitsHintShownOnFooter(t *testing.T) {
	s := keyedList(SelectListOptions{Footer: "y/n · Esc denies"})
	require.Contains(t, strings.Join(s.frameLines(80), "\n"), "y/n · Esc denies")
	s.hintOnFooter = true
	require.NotContains(t, strings.Join(s.frameLines(80), "\n"), "y/n · Esc denies")
}
