package tui

import (
	"sort"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

type teamRollup struct {
	path          string
	total         int
	running       int
	done          int
	blocked       int
	activeWorkers int
	providers     map[string]int
}

func hasTeamTasks(tasks []store.Task) bool {
	for _, task := range tasks {
		if task.TeamPath != "" {
			return true
		}
	}
	return false
}

func teamRollupsFromTasks(tasks []store.Task) []teamRollup {
	byPath := map[string]*teamRollup{}
	for _, task := range tasks {
		if task.TeamPath == "" {
			continue
		}
		parts := strings.Split(task.TeamPath, "/")
		for i := range parts {
			path := strings.Join(parts[:i+1], "/")
			rollup := byPath[path]
			if rollup == nil {
				rollup = &teamRollup{path: path, providers: map[string]int{}}
				byPath[path] = rollup
			}
			rollup.total++
			switch task.Status {
			case store.TaskStatusRunning:
				rollup.running++
				rollup.activeWorkers++
			case store.TaskStatusDone, store.TaskStatusSkipped, store.TaskStatusDecomposed:
				rollup.done++
			case store.TaskStatusBlocked, store.TaskStatusBlockedCapability, store.TaskStatusBlockedInput:
				rollup.blocked++
			}
			alias := task.AssignedAlias
			if alias == "" {
				alias = task.AssignedProvider
			}
			if alias != "" {
				rollup.providers[alias]++
			}
		}
	}
	out := make([]teamRollup, 0, len(byPath))
	for _, rollup := range byPath {
		out = append(out, *rollup)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func tasksForTeam(tasks []store.Task, team string) []store.Task {
	var out []store.Task
	for _, task := range tasks {
		if task.TeamPath == team || strings.HasPrefix(task.TeamPath, team+"/") {
			out = append(out, task)
		}
	}
	return out
}
