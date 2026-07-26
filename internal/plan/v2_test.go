package plan

import (
	"fmt"
	"strings"
	"testing"
)

const testSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func v2Step(text, metadata string) string {
	return fmt.Sprintf("- %s\n\n  ```ralph-task\n  %s\n  ```\n", text, metadata)
}

func TestParseV2StrictMetadataAndStableIdentity(t *testing.T) {
	md := "# Design\n\n" + v2Step("Audit the story", `{
    "id":"qfc.audit.story",
    "after":[],
    "team":"preproduction/human-causality","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},
    "requires":["local-agent"],
    "providers":["claude","codex"],
    "differentFrom":[],
    "inputs":[{"path":"docs/story.md","sha256":"`+testSHA+`"}],
    "outputs":[{"path":".qfc/packets/story.json","mode":"exclusive"}]
  }`)
	parsed, err := Parse([]byte(md))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !parsed.V2 {
		t.Fatal("V2 = false, want true")
	}
	tasks := parsed.V2Tasks()
	if len(tasks) != 1 || tasks[0].Step.Metadata == nil {
		t.Fatalf("tasks = %+v, want one metadata-bearing task", tasks)
	}
	if tasks[0].Step.Metadata.ID != "qfc.audit.story" {
		t.Fatalf("id = %q", tasks[0].Step.Metadata.ID)
	}
}

func TestValidateForImportRejectsV2AdversarialInputs(t *testing.T) {
	base := func(id, after, output string) string {
		return v2Step(id, fmt.Sprintf(`{
    "id":%q,"after":%s,"team":"design/team","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},"requires":[],
    "providers":["claude"],"differentFrom":[],
    "inputs":[],"outputs":[{"path":%q,"mode":"exclusive"}]
  }`, id, after, output))
	}
	tests := map[string]string{
		"duplicate id": "# Wave\n\n" +
			base("task.one", `[]`, "out/one") + "\n" +
			base("task.one", `[]`, "out/two"),
		"unknown dependency": "# Wave\n\n" +
			base("task.one", `["task.missing"]`, "out/one"),
		"cycle": "# Wave\n\n" +
			base("task.one", `["task.two"]`, "out/one") + "\n" +
			base("task.two", `["task.one"]`, "out/two"),
		"path traversal": "# Wave\n\n" +
			base("task.one", `[]`, "../escape"),
		"parallel output overlap": "# Wave\n\n" +
			base("task.one", `[]`, "out/shared") + "\n" +
			base("task.two", `[]`, "out/shared/file.json"),
		"mixed legacy and v2": "# Wave\n\n" +
			base("task.one", `[]`, "out/one") + "\n- legacy step\n",
		"unknown field": "# Wave\n\n" +
			v2Step("bad", `{"id":"task.one","after":[],"team":"design/team","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},"requires":[],"providers":[],"differentFrom":[],"inputs":[],"outputs":[],"surprise":true}`),
		"missing field": "# Wave\n\n" +
			v2Step("bad", `{"id":"task.one","after":[],"team":"design/team","requires":[],"providers":[],"differentFrom":[],"inputs":[]}`),
		"null field": "# Wave\n\n" +
			v2Step("bad", `{"id":"task.one","after":null,"team":"design/team","requires":[],"providers":[],"differentFrom":[],"inputs":[],"outputs":[]}`),
		"portable traversal": "# Wave\n\n" +
			base("task.one", `[]`, `..\escape`),
		"differentFrom self": "# Wave\n\n" +
			v2Step("bad", `{"id":"task.one","after":[],"team":"design/team","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},"requires":[],"providers":[],"differentFrom":["task.one"],"inputs":[],"outputs":[{"path":"out/self","mode":"exclusive"}]}`),
		"unordered read write": "# Wave\n\n" +
			base("task.writer", `[]`, "shared/data.json") + "\n" +
			v2Step("reader", `{"id":"task.reader","after":[],"team":"design/team","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},"requires":[],"providers":[],"differentFrom":[],"inputs":[{"path":"shared/data.json","sha256":"`+testSHA+`"}],"outputs":[{"path":"out/reader","mode":"exclusive"}]}`),
		"calibrated without content address": "# Wave\n\n" +
			v2Step("bad", `{"id":"task.one","after":[],"team":"design/team","binding":{"mode":"calibrated","alias":"codex-sol-xhigh","provider":"codex","model":"gpt-5.6-sol","effort":"xhigh","calibration":"","repetitions":0,"fixture":""},"requires":[],"providers":["codex"],"differentFrom":[],"inputs":[],"outputs":[{"path":"out/calibrated","mode":"exclusive"}]}`),
		"calibration requiring unmeasured capability": "# Wave\n\n" +
			v2Step("bad", `{"id":"task.one","after":[],"team":"design/team","binding":{"mode":"calibration","alias":"codex-sol-xhigh","provider":"codex","model":"gpt-5.6-sol","effort":"xhigh","calibration":"","repetitions":3,"fixture":"graph-reasoning"},"requires":["quality.graph-reasoning"],"providers":["codex"],"differentFrom":[],"inputs":[],"outputs":[{"path":"out/calibration","mode":"exclusive"}]}`),
		"missing outputs": "# Wave\n\n" +
			v2Step("bad", `{"id":"task.one","after":[],"team":"design/team","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},"requires":[],"providers":[],"differentFrom":[],"inputs":[],"outputs":[]}`),
	}
	for name, md := range tests {
		t.Run(name, func(t *testing.T) {
			if err := ValidateForImport([]byte(md)); err == nil {
				t.Fatal("ValidateForImport = nil, want rejection")
			}
		})
	}
}

func TestValidateForImportAllowsOrderedExclusiveOutputReuse(t *testing.T) {
	first := v2Step("first", `{
    "id":"task.first","after":[],"team":"design/team","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},"requires":[],
    "providers":[],"differentFrom":[],"inputs":[],
    "outputs":[{"path":"out/shared.json","mode":"exclusive"}]
  }`)
	second := v2Step("second", `{
    "id":"task.second","after":["task.first"],"team":"design/team","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},"requires":[],
    "providers":[],"differentFrom":[],"inputs":[],
    "outputs":[{"path":"out/shared.json","mode":"exclusive"}]
  }`)
	if err := ValidateForImport([]byte("# Wave\n\n" + first + "\n" + second)); err != nil {
		t.Fatalf("ValidateForImport: %v", err)
	}
}

func TestValidateRelativePathUsesOnePortableCanonicalSpelling(t *testing.T) {
	for _, valid := range []string{
		"docs/story.md",
		".qfc/packets/story.json",
	} {
		t.Run("valid-"+valid, func(t *testing.T) {
			if err := validateRelativePath(valid); err != nil {
				t.Fatalf("validateRelativePath(%q): %v", valid, err)
			}
		})
	}
	for _, invalid := range []string{
		"",
		"/absolute",
		`\\server\share`,
		`C:/secret`,
		`foo\bar`,
		"../escape",
		"foo/../escape",
		"./relative",
		"foo//bar",
		"foo/./bar",
	} {
		t.Run("invalid-"+invalid, func(t *testing.T) {
			if err := validateRelativePath(invalid); err == nil {
				t.Fatalf("validateRelativePath(%q) succeeded", invalid)
			}
		})
	}
}

func TestValidateForImportRequiresOrderedProviderSeparation(t *testing.T) {
	first := v2Step("first", `{
    "id":"task.first","after":[],"team":"design/team","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},"requires":[],
    "providers":[],"differentFrom":[],"inputs":[],"outputs":[{"path":"out/first","mode":"exclusive"}]
  }`)
	second := v2Step("second", `{
    "id":"task.second","after":[],"team":"design/team","binding":{"mode":"pool","alias":"","provider":"","model":"","effort":"","calibration":"","repetitions":0,"fixture":""},"requires":[],
    "providers":[],"differentFrom":["task.first"],"inputs":[],"outputs":[{"path":"out/second","mode":"exclusive"}]
  }`)
	err := ValidateForImport([]byte("# Wave\n\n" + first + "\n" + second))
	if err == nil || !strings.Contains(err.Error(), "requires an after dependency path") {
		t.Fatalf("error = %v, want provider-separation ordering rejection", err)
	}
}

func TestValidateForImportPreservesLegacyPlans(t *testing.T) {
	md := "# First\n\n- alpha\n- beta\n\n# Second\n\n1. gamma\n2. delta\n"
	if err := ValidateForImport([]byte(md)); err != nil {
		t.Fatalf("legacy ValidateForImport: %v", err)
	}
	parsed, err := Parse([]byte(md))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.V2 {
		t.Fatal("legacy plan reported V2")
	}
	ready, parallel := Decompose(parsed, map[string]bool{})
	if !parallel || len(ready) != 2 || strings.Join([]string{ready[0].Text, ready[1].Text}, ",") != "alpha,beta" {
		t.Fatalf("legacy ready = %+v parallel=%v", ready, parallel)
	}
}
