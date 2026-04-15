package variant

// Shared test fixtures used across registry_test.go, profiles_test.go,
// and validate_test.go. Kept in its own file so no single test file
// carries the burden of these definitions plus its test cases.

// Known-good tool set — anything outside this is a typo in a profile.
var knownTools = map[string]struct{}{
	ToolAgent:      {},
	ToolBash:       {},
	ToolEdit:       {},
	ToolGlob:       {},
	ToolGrep:       {},
	ToolRead:       {},
	ToolWrite:      {},
	ToolTaskCreate: {},
	ToolTaskUpdate: {},
	ToolTaskList:   {},
}

// allVariantNames is the canonical order used by parametrized tests.
var allVariantNames = []Name{
	Blue, Grey, Green, Red, Professor, Fixit,
	Immortal, Savage, OldMan, WorldBreaker,
}
