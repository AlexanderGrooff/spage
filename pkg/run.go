package pkg

import (
	"fmt"
)

func Execute(graph Graph, inventoryFile string) error {
	var contexts map[string]HostContext
	if inventoryFile == "" {
		contexts = make(map[string]HostContext)
		fmt.Printf("No inventory file specified. Assuming target is this machine\n")
		contexts["localhost"] = HostContext{Host: Host{IsLocal: true, Host: "localhost"}, Facts: make(Facts), History: make(Facts)}
	} else {
		inventory, err := LoadInventory(inventoryFile)
		if err != nil {
			return fmt.Errorf("failed to load inventory: %w", err)
		}
		contexts, err = inventory.GetContextForRun()
		if err != nil {
			return fmt.Errorf("failed to get contexts for run: %w", err)
		}
	}

	var executedOnHost []map[string][]Task
	for executionLevel, taskOnLevel := range graph.Tasks {
		DebugOutput("Starting execution level %d\n", executionLevel)
		for _, task := range taskOnLevel {
			// TODO: execute in parallel
			executedOnHost = append(executedOnHost, make(map[string][]Task))
			for hostname, c := range contexts {
				// TODO: execute in parallel

				fmt.Printf("[%s - %s]:execute\n", hostname, task)
				output, err := task.ExecuteModule(c)
				c.History[task.Name] = output
				if task.Register != "" {
					c.Facts[task.Register] = output
				}
				executedOnHost[executionLevel][hostname] = append(executedOnHost[executionLevel][hostname], task)
				PPrintOutput(output, err)

				if err != nil {
					DebugOutput("error executing '%s': %v\n\nREVERTING\n\n", task, err)

					if err := RevertTasks(executedOnHost, contexts); err != nil {
						return fmt.Errorf("run failed: %w", err)
					}
					return fmt.Errorf("reverted all tasks")
				}
			}
		}
	}

	DebugOutput("All tasks executed successfully")
	return nil
}

func RevertTasks(executedTasks []map[string][]Task, contexts map[string]HostContext) error {
	// Revert all tasks per level in descending order
	for executionLevel := len(executedTasks) - 1; executionLevel >= 0; executionLevel-- {
		for hostname, tasks := range executedTasks[executionLevel] {
			// TODO: revert hosts in parallel per executionlevel
			for _, task := range tasks {
				c := contexts[hostname]
				fmt.Printf("[%s - %s]:revert\n", hostname, task)
				output, err := task.RevertModule(c)
				PPrintOutput(output, err)
				if err != nil {
					return fmt.Errorf("failed to revert %s: %v\n", task, err)
				}
			}
		}
	}
	return nil
}
