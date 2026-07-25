package plan

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateV2 checks identity, dependency, and exclusive-output invariants.
func ValidateV2(parsed *Plan) error {
	tasks := parsed.V2Tasks()
	metadataCount := 0
	for _, task := range tasks {
		if task.Step.Metadata != nil {
			metadataCount++
		}
	}
	if metadataCount == 0 {
		return nil
	}
	if metadataCount != len(tasks) {
		return fmt.Errorf("plan v2 metadata is mixed with legacy steps; every step needs one ralph-task block")
	}

	byID := make(map[string]TaskMetadata, len(tasks))
	for _, task := range tasks {
		metadata := *task.Step.Metadata
		if err := validateTaskMetadata(&metadata); err != nil {
			return err
		}
		if _, exists := byID[metadata.ID]; exists {
			return fmt.Errorf("duplicate stable task id %q", metadata.ID)
		}
		byID[metadata.ID] = metadata
	}
	for id, metadata := range byID {
		for _, dependency := range append(metadata.After, metadata.DifferentFrom...) {
			if _, exists := byID[dependency]; !exists {
				return fmt.Errorf("task %s references unknown task %s", id, dependency)
			}
		}
	}
	if cycle := dependencyCycle(byID); len(cycle) > 0 {
		return fmt.Errorf("plan v2 dependency cycle: %s", strings.Join(cycle, " -> "))
	}
	for id, metadata := range byID {
		for _, separated := range metadata.DifferentFrom {
			if !transitivelyDepends(byID, id, separated) {
				return fmt.Errorf(
					"task %s differentFrom %s requires an after dependency path",
					id, separated,
				)
			}
		}
	}
	return validateOutputOverlaps(byID)
}

func dependencyCycle(tasks map[string]TaskMetadata) []string {
	const visiting, visited = 1, 2
	state := map[string]int{}
	var stack []string
	var visit func(string) []string
	visit = func(id string) []string {
		if state[id] == visiting {
			for i := range stack {
				if stack[i] == id {
					return append(append([]string{}, stack[i:]...), id)
				}
			}
		}
		if state[id] == visited {
			return nil
		}
		state[id] = visiting
		stack = append(stack, id)
		for _, dependency := range tasks[id].After {
			if cycle := visit(dependency); len(cycle) > 0 {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = visited
		return nil
	}
	for id := range tasks {
		if cycle := visit(id); len(cycle) > 0 {
			return cycle
		}
	}
	return nil
}

func validateOutputOverlaps(tasks map[string]TaskMetadata) error {
	type ownedPath struct {
		task string
		path string
	}
	var paths []ownedPath
	for id, metadata := range tasks {
		for _, output := range metadata.Outputs {
			paths = append(paths, ownedPath{task: id, path: filepath.Clean(output.Path)})
		}
	}
	for i := range paths {
		for j := i + 1; j < len(paths); j++ {
			if !pathsOverlap(paths[i].path, paths[j].path) {
				continue
			}
			if transitivelyDepends(tasks, paths[i].task, paths[j].task) ||
				transitivelyDepends(tasks, paths[j].task, paths[i].task) {
				continue
			}
			return fmt.Errorf(
				"exclusive outputs overlap without dependency ordering: %s:%s and %s:%s",
				paths[i].task, paths[i].path, paths[j].task, paths[j].path,
			)
		}
	}
	return nil
}

func pathsOverlap(left, right string) bool {
	left = strings.ToLower(filepath.ToSlash(filepath.Clean(left)))
	right = strings.ToLower(filepath.ToSlash(filepath.Clean(right)))
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func transitivelyDepends(tasks map[string]TaskMetadata, task, candidate string) bool {
	seen := map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if id == candidate {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, dependency := range tasks[id].After {
			if visit(dependency) {
				return true
			}
		}
		return false
	}
	return visit(task)
}
