package plan

// Plan is the parsed, nested representation of a plan document.
type Plan struct {
	// Groups holds the top-level groups in dependency order.
	Groups []Group

	// V2 is true when every step carries strict ralph-task JSON metadata.
	V2 bool
}

// Group is a heading section. It carries either Steps or SubGroups.
type Group struct {
	Heading   string
	Level     int
	Parallel  bool
	Steps     []Step
	SubGroups []Group
}

// Step is one list item plus its optional narrative detail and metadata.
type Step struct {
	Text             string
	Detail           string
	RequiresApproval bool

	// Metadata opts this step into stable identity and explicit DAG semantics.
	// Nil preserves the legacy heading/list execution contract.
	Metadata *TaskMetadata
}
