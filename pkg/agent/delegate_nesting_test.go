package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetMaxDelegateNestingDepth_Default(t *testing.T) {
	// Ensure env var is not set
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, MaxDelegateNestingDepth, depth)
	assert.Equal(t, 3, depth)
}

func TestGetMaxDelegateNestingDepth_CustomValue(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "5")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, 5, depth)
}

func TestGetMaxDelegateNestingDepth_CustomValueOne(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "1")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, 1, depth)
}

func TestGetMaxDelegateNestingDepth_CustomValueLarge(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "100")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, 100, depth)
}

func TestGetMaxDelegateNestingDepth_NonNumeric(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "abc")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, MaxDelegateNestingDepth, depth)
}

func TestGetMaxDelegateNestingDepth_NonNumericSpecialChars(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "5.5")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, MaxDelegateNestingDepth, depth)
}

func TestGetMaxDelegateNestingDepth_NonNumericWithSpaces(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", " 5 ")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, MaxDelegateNestingDepth, depth)
}

func TestGetMaxDelegateNestingDepth_Zero(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "0")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, MaxDelegateNestingDepth, depth)
}

func TestGetMaxDelegateNestingDepth_Negative(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "-1")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, MaxDelegateNestingDepth, depth)
}

func TestGetMaxDelegateNestingDepth_NegativeLarge(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "-100")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, MaxDelegateNestingDepth, depth)
}

func TestGetMaxDelegateNestingDepth_EmptyString(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "")

	depth := getMaxDelegateNestingDepth()
	assert.Equal(t, MaxDelegateNestingDepth, depth)
}

func TestGetMaxDelegateNestingDepth_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     int
	}{
		{"default empty", "", 3},
		{"valid 1", "1", 1},
		{"valid 2", "2", 2},
		{"valid 3", "3", 3},
		{"valid 5", "5", 5},
		{"valid 10", "10", 10},
		{"invalid zero", "0", 3},
		{"invalid negative", "-1", 3},
		{"invalid text", "abc", 3},
		{"invalid float", "3.14", 3},
		{"invalid empty spaces", "   ", 3},
		{"invalid special", "#$%", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", tt.envValue)
			got := getMaxDelegateNestingDepth()
			assert.Equal(t, tt.want, got, "SPROUT_MAX_DELEGATE_DEPTH=%q", tt.envValue)
		})
	}
}

func TestGetMaxDelegateNestingDepth_ConcurrentEnvChanges(t *testing.T) {
	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "5")
	assert.Equal(t, 5, getMaxDelegateNestingDepth())

	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "10")
	assert.Equal(t, 10, getMaxDelegateNestingDepth())

	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "")
	assert.Equal(t, 3, getMaxDelegateNestingDepth())

	t.Setenv("SPROUT_MAX_DELEGATE_DEPTH", "7")
	assert.Equal(t, 7, getMaxDelegateNestingDepth())
}
