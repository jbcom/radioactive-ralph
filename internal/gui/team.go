//go:build gui

package gui

import (
	"sort"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

type guiTeamRollup struct {
	path, provider                string
	total, running, blocked, done int
}

func guiHasTeams(tasks []store.Task) bool {
	for _, task := range tasks {
		if task.TeamPath != "" {
			return true
		}
	}
	return false
}

func guiTeamRollups(tasks []store.Task) []guiTeamRollup {
	byPath := map[string]*guiTeamRollup{}
	for _, task := range tasks {
		if task.TeamPath == "" {
			continue
		}
		parts := strings.Split(task.TeamPath, "/")
		for i := range parts {
			path := strings.Join(parts[:i+1], "/")
			rollup := byPath[path]
			if rollup == nil {
				rollup = &guiTeamRollup{path: path}
				byPath[path] = rollup
			}
			rollup.total++
			switch task.Status {
			case store.TaskStatusRunning:
				rollup.running++
			case store.TaskStatusDone, store.TaskStatusSkipped, store.TaskStatusDecomposed:
				rollup.done++
			case store.TaskStatusBlocked, store.TaskStatusBlockedCapability, store.TaskStatusBlockedInput:
				rollup.blocked++
			}
			if task.AssignedAlias != "" {
				rollup.provider = task.AssignedAlias
			} else if task.AssignedProvider != "" {
				rollup.provider = task.AssignedProvider
			}
		}
	}
	out := make([]guiTeamRollup, 0, len(byPath))
	for _, rollup := range byPath {
		out = append(out, *rollup)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func guiTasksForTeam(tasks []store.Task, team string) []store.Task {
	var out []store.Task
	for _, task := range tasks {
		if task.TeamPath == team || strings.HasPrefix(task.TeamPath, team+"/") {
			out = append(out, task)
		}
	}
	return out
}
