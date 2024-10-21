package pkg

import (
	"fmt"
)

func Reconcile(tasks []Task, inventoryFile string) error {
	executed := []Task{}
	for _, task := range tasks {
		fmt.Printf("Executing %s\n", task)
		err := task.ExecuteModule()
		if err != nil {
			fmt.Printf("Error executing %s: %v\n", task, err)
			fmt.Println("Reverting tasks...")

			if err := RevertTasks(executed); err != nil {
				return fmt.Errorf("reconciliation failed: %w", err)
			}
			return fmt.Errorf("reverted all tasks")
		}
		executed = append(executed, task)
	}

	fmt.Println("All tasks executed successfully")
	return nil
}

func RevertTasks(tasks []Task) error {
	// Revert tasks in reverse order
	for j := len(tasks) - 1; j >= 0; j-- {
		fmt.Printf("Reverting %s\n", tasks[j])
		if revertErr := tasks[j].RevertModule(); revertErr != nil {
			fmt.Printf("Error reverting %s: %v\n", tasks[j], revertErr)
		}
	}
	return nil
}
