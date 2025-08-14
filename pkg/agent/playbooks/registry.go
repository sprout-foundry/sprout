package playbooks

// init registers all standard playbooks for selection by the agent.
func init() {
	// Documentation and comments
	Register(DocsAuditPlaybook{})
	Register(HeaderSummaryPlaybook{})
	Register(CodeCommentSyncPlaybook{})

	// Small deterministic edits
	Register(SimpleReplacePlaybook{})
	Register(FileRenamePlaybook{})
	Register(MultiFileEditPlaybook{})

	// Fixes
	Register(BugFixCompilationPlaybook{})
	Register(LintFixPlaybook{})
	Register(SecurityAuditFixPlaybook{})

	// Testing
	Register(TestFailureFixPlaybook{})
	Register(AddUnitTestsPlaybook{})

	// Refactors & migrations
	Register(RefactorSmallPlaybook{})
	Register(APIChangePropagationPlaybook{})
	Register(TypesMigrationPlaybook{})

	// Features & config
	Register(FeatureToggleSmallPlaybook{})
	Register(DependencyUpdatePlaybook{})
	Register(ConfigChangePlaybook{})

	// Ops & quality
	Register(CIUpdatePlaybook{})
	Register(PerformanceOptimizeHotPathPlaybook{})
	Register(LoggingImprovePlaybook{})
}
