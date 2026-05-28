package api

import (
	"reflect"
	"testing"
)

func TestClassifyEligibleRoles(t *testing.T) {
	cases := []struct {
		ctx  int
		want []string
	}{
		{300000, []string{"primary", "subagent"}},
		{128000, []string{"primary", "subagent"}},
		{127999, []string{"subagent"}},
		{32000, []string{"subagent"}},
		{31999, nil},
		{0, nil}, // unknown context
	}
	for _, c := range cases {
		got := ClassifyEligibleRoles(ModelInfo{ContextLength: c.ctx})
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ClassifyEligibleRoles(ctx=%d) = %v, want %v", c.ctx, got, c.want)
		}
	}
}

func TestFillEligibleRoles_PreservesExisting(t *testing.T) {
	models := []ModelInfo{
		{ID: "big", ContextLength: 200000},                                  // → filled primary+subagent
		{ID: "small", ContextLength: 8000},                                  // → stays empty (below bar)
		{ID: "probed", ContextLength: 8000, EligibleRoles: []string{"x"}},  // → preserved (already set)
	}
	out := fillEligibleRoles(models)

	if !reflect.DeepEqual(out[0].EligibleRoles, []string{"primary", "subagent"}) {
		t.Errorf("big: got %v", out[0].EligibleRoles)
	}
	if len(out[1].EligibleRoles) != 0 {
		t.Errorf("small: expected empty, got %v", out[1].EligibleRoles)
	}
	if !reflect.DeepEqual(out[2].EligibleRoles, []string{"x"}) {
		t.Errorf("probed: existing roles not preserved, got %v", out[2].EligibleRoles)
	}
}
