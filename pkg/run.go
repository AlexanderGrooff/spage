package pkg

import (
	"fmt"
)

func Execute(graph Graph, inventoryFile string) error {
	var contexts map[string]Context
	if inventoryFile == "" {
		contexts = make(map[string]Context)
		fmt.Printf("No inventory file specified. Assuming target is this machine\n")
		contexts["localhost"] = Context{Host: Host{IsLocal: true, Host: "localhost"}, Facts: make(Facts), History: make(Facts)}
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

	executed := make([][]Task, len(graph.Tasks))
	for executionLevel, taskOnLevel := range graph.Tasks {
		fmt.Printf("Starting execution level %d\n\n", executionLevel)
		for _, task := range taskOnLevel {
			// TODO: execute in parallel
			for hostname, c := range contexts {
				// TODO: execute in parallel

				fmt.Printf("[%s - %s]:execute\n", hostname, task)
				output, err := task.ExecuteModule(c)
				c.History[task.Name] = output
				if task.Register != "" {
					c.Facts[task.Register] = output
				}
				executed[executionLevel] = append(executed[executionLevel], task)

				if err != nil {
					if output == nil {
						fmt.Printf("  \033[31m%s\033[0m\n", err)
					} else {
						fmt.Printf("\033[31m%s\033[0m\n", output.String())
					}
				} else if output.Changed() {
					// Yellow
					fmt.Printf("\033[33m%s\033[0m\n", output.String())
				} else {
					// Green
					fmt.Printf("\033[32m%s\033[0m\n", output.String())
				}

				if err != nil {
					fmt.Printf("error executing '%s': %v\n\nREVERTING\n\n", task, err)

					if err := RevertTasks(executed, contexts); err != nil {
						return fmt.Errorf("run failed: %w", err)
					}
					return fmt.Errorf("reverted all tasks")
				}
			}
		}
		fmt.Println()
	}

	fmt.Println("All tasks executed successfully")
	return nil
}

func RevertTasks(taskLevels [][]Task, contexts map[string]Context) error {
	// Revert all tasks per level in descending order
	for j := len(taskLevels) - 1; j >= 0; j-- {
		tasks := taskLevels[j]
		for _, task := range tasks {
			for hostname, c := range contexts {
				fmt.Printf("[%s - %s]:revert\n", hostname, task)
				output, revertErr := task.RevertModule(c)
				if revertErr != nil {
					fmt.Printf("error reverting %s: %v\n", task, revertErr)
				}
				fmt.Printf("%s\n", output.String())
			}
		}
	}
	return nil
}
