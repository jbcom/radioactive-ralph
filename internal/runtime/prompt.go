package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/plandag"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

type promptContext struct {
	Variant variant.Profile
	Plan    plandag.Plan
	Task    plandag.Task
	Repo    string
}

func buildWorkerSystemPrompt(ctx promptContext) string {
	var out strings.Builder
	out.WriteString(renderVariantSystemPrompt(ctx.Variant))
	out.WriteString("\n\n")
	fmt.Fprintf(&out, "You are executing task %q from the radioactive_ralph repo service.\n", ctx.Task.ID)
	fmt.Fprintf(&out, "Repo: %s\n", ctx.Repo)
	fmt.Fprintf(&out, "Plan: %s (%s)\n", ctx.Plan.Title, ctx.Plan.Slug)
	fmt.Fprintf(&out, "Variant operating mode: %s\n", ctx.Variant.Name)
	out.WriteString("\nRules:\n")
	out.WriteString("- Work only on the claimed task and its direct acceptance criteria.\n")
	out.WriteString("- Use the allowed tools naturally; radioactive_ralph already scoped the work and runtime context for you.\n")
	out.WriteString("- If the task is complete, emit outcome `done`.\n")
	out.WriteString("- If you are blocked by missing operator approval or a risky next step, emit outcome `need_operator`.\n")
	out.WriteString("- If another Ralph variant is a better fit, emit outcome `handoff` and name `handoff_to`.\n")
	out.WriteString("- If you need the operator/runtime to fetch more context before proceeding, emit outcome `need_context` with `needs_context` entries.\n")
	out.WriteString("- If you are blocked by an external dependency or unresolved prerequisite, emit outcome `blocked`.\n")
	out.WriteString("- If the task genuinely failed, emit outcome `failed` with a concise reason.\n")
	out.WriteString("- Respond with JSON only. No prose before or after the JSON object.\n")
	out.WriteString("\nOutput schema:\n")
	out.WriteString("{\"outcome\":\"done|failed|need_operator|handoff|blocked|need_context\",\"summary\":\"...\",\"evidence\":[\"...\"],\"reason\":\"...\",\"handoff_to\":\"variant-name\",\"approval_required\":true|false,\"retryable\":true|false,\"needs_context\":[\"...\"]}\n")
	return strings.TrimSpace(out.String())
}

func renderVariantSystemPrompt(profile variant.Profile) string {
	var out strings.Builder
	fmt.Fprintf(&out, "You are running under radioactive-ralph as variant %q.\n", profile.Name)
	fmt.Fprintf(&out, "Description: %s\n", profile.Description)
	for _, directive := range profile.PromptDirectives {
		directive = strings.TrimSpace(directive)
		if directive == "" {
			continue
		}
		fmt.Fprintf(&out, "\n- %s", directive)
	}
	return strings.TrimSpace(out.String())
}

func buildWorkerUserPrompt(ctx promptContext) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Task: %s\n", ctx.Task.Description)
	if ctx.Task.VariantHint != "" {
		fmt.Fprintf(&out, "Variant hint: %s\n", ctx.Task.VariantHint)
	}
	if ctx.Task.Complexity != "" || ctx.Task.Effort != "" {
		fmt.Fprintf(&out, "Estimated size: complexity=%s effort=%s\n", ctx.Task.Complexity, ctx.Task.Effort)
	}
	if strings.TrimSpace(ctx.Task.AcceptanceJSON) != "" {
		var criteria []string
		if err := json.Unmarshal([]byte(ctx.Task.AcceptanceJSON), &criteria); err == nil && len(criteria) > 0 {
			out.WriteString("Acceptance criteria:\n")
			for _, item := range criteria {
				fmt.Fprintf(&out, "- %s\n", item)
			}
		}
	}
	out.WriteString("\nExecute the task now and return only the JSON object.\n")
	return out.String()
}
