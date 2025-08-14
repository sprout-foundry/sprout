package playbooks

import "testing"

func TestPlaybookMatches(t *testing.T) {
	// Docs-related
	if !(HeaderSummaryPlaybook{}).Matches("add header summary", "docs") {
		t.Fatalf("header summary should match docs")
	}
	if !(CodeCommentSyncPlaybook{}).Matches("update comments only", "") {
		t.Fatalf("comment sync should match")
	}

	// Simple replace
	if !(SimpleReplacePlaybook{}).Matches("change 'a' to 'b'", "") {
		t.Fatalf("simple replace should match")
	}

	// Fix/build
	if !(BugFixCompilationPlaybook{}).Matches("compile error: undefined: X", "") {
		t.Fatalf("fix build should match compile error")
	}
	if !(TestFailureFixPlaybook{}).Matches("--- FAIL: TestAbc", "") {
		t.Fatalf("test failure fix should match")
	}

	// Add tests
	if !(AddUnitTestsPlaybook{}).Matches("increase coverage", "test") {
		t.Fatalf("add tests should match")
	}

	// Refactor/migration
	if !(RefactorSmallPlaybook{}).Matches("refactor method", "") {
		t.Fatalf("refactor small should match")
	}
	if !(APIChangePropagationPlaybook{}).Matches("signature change of DoThing", "") {
		t.Fatalf("api change should match")
	}
	if !(TypesMigrationPlaybook{}).Matches("replace type Foo with Bar", "") {
		t.Fatalf("types migration should match")
	}

	// Features & config
	if !(FeatureToggleSmallPlaybook{}).Matches("add flag --fast", "") {
		t.Fatalf("feature toggle should match")
	}
	if !(DependencyUpdatePlaybook{}).Matches("upgrade package", "") {
		t.Fatalf("dependency update should match")
	}
	if !(ConfigChangePlaybook{}).Matches("update configuration default", "") {
		t.Fatalf("config change should match")
	}

	// Ops & quality
	if !(CIUpdatePlaybook{}).Matches("update CI workflow", "") {
		t.Fatalf("ci update should match")
	}
	if !(PerformanceOptimizeHotPathPlaybook{}).Matches("optimize performance", "") {
		t.Fatalf("perf opt should match")
	}
	if !(SecurityAuditFixPlaybook{}).Matches("sanitize credentials", "") {
		t.Fatalf("security fix should match")
	}
	if !(LoggingImprovePlaybook{}).Matches("improve logging", "") {
		t.Fatalf("logging improve should match")
	}
}
