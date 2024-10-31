package pkg

import (
	"fmt"
	"sync"
)

func Execute(graph Graph, inventoryFile string) error {
	var contexts map[string]*HostContext
	if inventoryFile == "" {
		contexts = make(map[string]*HostContext)
		fmt.Printf("No inventory file specified. Assuming target is this machine\n")
		contexts["localhost"] = &HostContext{Host: Host{Name: "localhost", IsLocal: true, Host: "localhost"}, Facts: make(Facts), History: make(map[string]ModuleOutput)}
	} else {
		inventory, err := LoadInventory(inventoryFile)
		if err != nil {
			return fmt.Errorf("failed to load inventory: %w", err)
		}
		DebugOutput("Getting contexts for run from inventory %+v", inventory)
		contexts, err = inventory.GetContextForRun()
		if err != nil {
			return fmt.Errorf("failed to get contexts for run: %w", err)
		}
	}

	var executedOnHost []map[string][]Task
	for executionLevel, tasksOnLevel := range graph.Tasks {
		DebugOutput("Starting execution level %d\n", executionLevel)
		// Run all tasks of this level on all hosts in parallel, regardless of errors
		numExpectedResults := len(tasksOnLevel) * len(contexts)
		ch := make(chan TaskResult, numExpectedResults)
		var wg sync.WaitGroup
		for _, task := range tasksOnLevel {
			executedOnHost = append(executedOnHost, make(map[string][]Task))

			for _, c := range contexts {
				// if executedOnHost[executionLevel][hostname] == nil {
				// 	DebugOutput("Creating new slice for %q", hostname)
				// 	executedOnHost[executionLevel][hostname] = []Task{}
				// }
				wg.Add(1)
				go func(task Task, c *HostContext) {
					defer wg.Done()
					ch <- task.ExecuteModule(c)
				}(task, c)
			}
		}
		go func() {
			wg.Wait()
			close(ch)
		}()

		// Process results
		var errored bool
		for result := range ch {
			hostname := result.Context.Host.Name
			task := result.Task
			c := result.Context
			fmt.Printf("[%s - %s]:execute\n", c.Host.Host, task.Name)
			c.History[task.Name] = result.Output
			if task.Register != "" {
				c.Facts[task.Register] = result.Output
			}
			executedOnHost[executionLevel][hostname] = append(executedOnHost[executionLevel][hostname], task)
			PPrintOutput(result.Output, result.Error)

			if result.Error != nil {
				DebugOutput("error executing '%s': %v\n\nREVERTING\n\n", task, result.Error)
				errored = true
			}
		}
		if errored {
			if err := RevertTasks(executedOnHost, contexts); err != nil {
				return fmt.Errorf("run failed: %w", err)
			}
			return fmt.Errorf("reverted all tasks")
		}
	}

	DebugOutput("All tasks executed successfully")
	return nil
}

func RevertTasks(executedTasks []map[string][]Task, contexts map[string]*HostContext) error {
	// Revert all tasks per level in descending order
	for executionLevel := len(executedTasks) - 1; executionLevel >= 0; executionLevel-- {
		for hostname, tasks := range executedTasks[executionLevel] {
			// TODO: revert hosts in parallel per executionlevel
			for _, task := range tasks {
				c, ok := contexts[hostname]
				if !ok {
					return fmt.Errorf("context for %q not found", hostname)
				}
				tOutput := task.RevertModule(c)
				PPrintOutput(tOutput.Output, tOutput.Error)
				if tOutput.Error != nil {
					return fmt.Errorf("failed to revert %s: %v\n", task, tOutput.Error)
				}
			}
		}
	}
	return nil
}
