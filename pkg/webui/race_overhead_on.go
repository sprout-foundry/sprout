//go:build race

package webui

// raceOverheadFactor scales memory-growth bounds when the test binary
// runs under the race detector. -race inflates heap traffic 5-10x (a
// shadow copy per access plus bookkeeping), so an absolute "generous"
// bound measured on a normal binary trips spuriously on a race build —
// 288MB observed against a 256MB bound that passes without -race.
const raceOverheadFactor = 8
