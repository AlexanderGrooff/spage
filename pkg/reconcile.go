package pkg

import (
	"fmt"
)

func Reconcile(graph Graph, inventoryFile string) error {
	runtimeVariables := make(map[string]interface{})
	executed := make([][]Task, len(graph.Tasks))
	for executionLevel, taskOnLevel := range graph.Tasks {
		fmt.Printf("Starting execution level %d\n", executionLevel)
		for _, task := range taskOnLevel {
			// TODO: execute in parallel
			fmt.Printf("Executing %s\n", task)
			output, err := task.ExecuteModule()
			runtimeVariables[task.Register] = output
			if err != nil {
				fmt.Printf("Error executing %s: %v\n", task, err)
				fmt.Println("Reverting tasks...")

				if err := RevertTasks(executed); err != nil {
					return fmt.Errorf("reconciliation failed: %w", err)
				}
				return fmt.Errorf("reverted all tasks")
			}
			executed[executionLevel] = append(executed[executionLevel], task)
		}
	}

	fmt.Println("All tasks executed successfully")
	return nil
}

func RevertTasks(taskLevels [][]Task) error {
	// Revert all tasks per level in descending order
	for j := len(taskLevels) - 1; j >= 0; j-- {
		tasks := taskLevels[j]
		for _, task := range tasks {
			fmt.Printf("Reverting %s\n", tasks[j])
			if _, revertErr := task.RevertModule(); revertErr != nil {
				fmt.Printf("Error reverting %s: %v\n", task, revertErr)
			}
		}
	}
	return nil
}
