//go:build !race

package webui

// raceOverheadFactor is 1 without the race detector — bounds measured
// on a normal binary apply unchanged. See race_overhead_on.go.
const raceOverheadFactor = 1
