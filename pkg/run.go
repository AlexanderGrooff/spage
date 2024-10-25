package pkg

import (
	"fmt"
)

func Execute(graph Graph, inventoryFile string) error {
	c := make(Context)
	executed := make([][]Task, len(graph.Tasks))
	for executionLevel, taskOnLevel := range graph.Tasks {
		fmt.Printf("Starting execution level %d\n\n", executionLevel)
		for _, task := range taskOnLevel {
			// TODO: execute in parallel
			fmt.Printf("[%s]:execute\n", task)
			output, err := task.ExecuteModule(c)
			c[task.Register] = output
			executed[executionLevel] = append(executed[executionLevel], task)
			if err != nil {
				fmt.Printf("error executing '%s': %v\n\nREVERTING\n\n", task, err)

				if err := RevertTasks(executed, c); err != nil {
					return fmt.Errorf("run failed: %w", err)
				}
				return fmt.Errorf("reverted all tasks")
			}
			fmt.Printf("%s\n", output.String())
		}
		fmt.Println()
	}

	fmt.Println("All tasks executed successfully")
	return nil
}

func RevertTasks(taskLevels [][]Task, c Context) error {
	// Revert all tasks per level in descending order
	for j := len(taskLevels) - 1; j >= 0; j-- {
		tasks := taskLevels[j]
		for _, task := range tasks {
			fmt.Printf("[%s]:revert\n", task)
			output, revertErr := task.RevertModule(c)
			if revertErr != nil {
				fmt.Printf("error reverting %s: %v\n", task, revertErr)
			}
			fmt.Printf("%s\n", output.String())
		}
	}
	return nil
}
