package agent

// SP-068 Phase 3 removed the Gate-1 → Gate-2 approval bridge plumbing
// (markShellCommandApproved, consumeShellCommandApproval, and the
// recentlyApprovedShellCommands map). The tests that exercised those
// functions are deleted here — the bridge is no longer needed because
// the unified risk resolver runs a single gate.
//
// The behavior those tests guarded against (double-prompting) is now
// structurally impossible: there is no Gate 2 to re-prompt. See
// sp068_regression_test.go for the regression tests that lock in the
// new architecture.
