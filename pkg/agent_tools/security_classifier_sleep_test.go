package tools

import (
	"strings"
	"testing"
)

func TestIsStandaloneSleepOrWaitCommand_MatchesPureForms(t *testing.T) {
	t.Parallel()
	cases := []string{
		// bare integer seconds
		"sleep 0",
		"sleep 1",
		"sleep 600",
		"sleep 99999",
		// decimal seconds
		"sleep 0.5",
		"sleep 3.14",
		// time suffixes (GNU sleep)
		"sleep 30s",
		"sleep 10m",
		"sleep 2h",
		"sleep 1d",
		// extra whitespace between sleep and arg
		"sleep   600",
		// bare wait
		"wait",
		"wait 12345",
	}
	for _, c := range cases {
		c := c
		t.Run(c, func(t *testing.T) {
			if !isStandaloneSleepOrWaitCommand(c) {
				t.Errorf("expected %q to be flagged as standalone sleep/wait", c)
			}
		})
	}
}

func TestIsStandaloneSleepOrWaitCommand_AllowsChainedAndEmbedded(t *testing.T) {
	t.Parallel()
	cases := []string{
		// chained — legitimate scripting
		"make build && sleep 5 && curl localhost:8080/healthz",
		"sleep 5 && echo done",
		"echo start; sleep 1; echo end",
		"do_thing || sleep 30 && retry",
		"cmd1 | grep foo | head -1",
		// inside a shell -c quoted script
		`bash -c "sleep 60"`,
		// inside a for loop
		"for i in 1 2 3; do sleep $i; done",
		// not sleep at all
		"echo hello",
		"git status",
		"sleep_helper",
		"sleeplessness",
		// sleep with non-numeric arg (e.g. variable, doesn't match the pattern)
		"sleep $DELAY",
		// wait with multi-pid args (not single-pid form)
		"wait 1 2 3",
		// wait followed by command continuation
		"wait && echo done",
		// empty / whitespace
		"",
		"   ",
	}
	for _, c := range cases {
		c := c
		t.Run(c, func(t *testing.T) {
			if isStandaloneSleepOrWaitCommand(strings.TrimSpace(c)) {
				t.Errorf("did not expect %q to be flagged as standalone sleep/wait", c)
			}
		})
	}
}

func TestClassifyShellCommand_SleepIsSafeNotSecurityBlock(t *testing.T) {
	t.Parallel()
	res := ClassifyToolCall("shell_command", map[string]interface{}{"command": "sleep 600"})
	// Standalone sleep is NOT a security issue — it's a usage guidance issue.
	// The classifier returns Safe so no security elevation triggers. The shell
	// handler catches it and returns a helpful tool error instead.
	if res.ShouldBlock {
		t.Fatalf("expected ShouldBlock=false for standalone sleep (no longer a security block), got %+v", res)
	}
	if res.IsHardBlock {
		t.Fatalf("expected IsHardBlock=false for standalone sleep, got %+v", res)
	}
	if res.Risk != SecuritySafe {
		t.Fatalf("expected SecuritySafe, got %v", res.Risk)
	}
	if !strings.Contains(res.Reasoning, "check_background") {
		t.Fatalf("reasoning must name the correct alternative (check_background/wait_seconds), got: %s", res.Reasoning)
	}
	if !strings.Contains(res.Reasoning, "wait_seconds") {
		t.Fatalf("reasoning must mention wait_seconds, got: %s", res.Reasoning)
	}
	if !strings.Contains(res.Reasoning, "chain with &&") {
		t.Fatalf("reasoning must explain the chained-sleep escape hatch, got: %s", res.Reasoning)
	}
}

func TestClassifyShellCommand_WaitIsSafeNotSecurityBlock(t *testing.T) {
	t.Parallel()
	res := ClassifyToolCall("shell_command", map[string]interface{}{"command": "wait"})
	if res.ShouldBlock {
		t.Fatalf("expected ShouldBlock=false for bare wait (no longer a security block), got %+v", res)
	}
	if res.IsHardBlock {
		t.Fatalf("expected IsHardBlock=false")
	}
}

func TestClassifyShellCommand_AllowsChainedSleep(t *testing.T) {
	t.Parallel()
	res := ClassifyToolCall("shell_command", map[string]interface{}{
		"command": "make build && sleep 2 && curl localhost:8080/healthz",
	})
	if res.ShouldBlock {
		t.Fatalf("chained sleep should NOT be blocked, got %+v", res)
	}
}

func TestClassifyShellCommand_AllowsShellScriptWithSleep(t *testing.T) {
	t.Parallel()
	res := ClassifyToolCall("shell_command", map[string]interface{}{
		"command": `bash -c "for i in 1 2 3; do sleep 1; done"`,
	})
	if res.ShouldBlock {
		t.Fatalf("scripted sleep should NOT be blocked, got %+v", res)
	}
}
