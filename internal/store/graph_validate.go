package store

import "fmt"

func validateGraphSpecs(tasks []GraphTaskSpec) error {
	byID := make(map[string]GraphTaskSpec, len(tasks))
	for _, task := range tasks {
		if task.ID == "" {
			return fmt.Errorf("store: graph task id required")
		}
		if _, exists := byID[task.ID]; exists {
			return fmt.Errorf("store: duplicate graph task %s", task.ID)
		}
		byID[task.ID] = task
		for _, output := range task.Outputs {
			if output.Mode != "exclusive" {
				return fmt.Errorf("store: graph task %s output mode must be exclusive", task.ID)
			}
		}
	}
	for _, task := range tasks {
		for _, dependency := range task.DependsOn {
			if dependency == task.ID {
				return fmt.Errorf("store: graph task %s depends on itself", task.ID)
			}
			if _, exists := byID[dependency]; !exists {
				return fmt.Errorf("store: graph task %s references missing dependency %s", task.ID, dependency)
			}
		}
	}

	const visiting, visited = 1, 2
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == visiting {
			return fmt.Errorf("store: graph contains dependency cycle at %s", id)
		}
		if state[id] == visited {
			return nil
		}
		state[id] = visiting
		for _, dependency := range byID[id].DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = visited
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
