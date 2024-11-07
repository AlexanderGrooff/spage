package pkg

import (
	"fmt"
	"sync"
)

// getTasks returns all tasks from a GraphNode, handling both TaskList and Graph types
func getTasks(node GraphNode) []Task {
	switch n := node.(type) {
	case TaskNode:
		return []Task{n.Task}
	case Graph:
		var tasks []Task
		for _, level := range n.Tasks {
			for _, node := range level {
				tasks = append(tasks, getTasks(node)...)
			}
		}
		return tasks
	case Task:
		return []Task{n}
	default:
		return nil
	}
}

func Execute(graph Graph, inventoryFile string) error {
	var inventory *Inventory
	var err error
	if inventoryFile == "" {
		fmt.Printf("No inventory file specified. Assuming target is this machine\n")
		inventory = &Inventory{Hosts: map[string]*Host{"localhost": {Name: "localhost", IsLocal: true, Host: "localhost"}}}
	} else {
		inventory, err = LoadInventory(inventoryFile)
		if err != nil {
			return fmt.Errorf("failed to load inventory: %w", err)
		}
		DebugOutput("Getting contexts for run from inventory %+v", inventory)
	}

	if err := graph.CheckInventoryForRequiredInputs(inventory); err != nil {
		return fmt.Errorf("failed to check inventory for required inputs: %w", err)
	}
	contexts, err := inventory.GetContextForRun()
	if err != nil {
		return fmt.Errorf("failed to get contexts for run: %w", err)
	}

	var executedOnHost []map[string][]Task
	for executionLevel, nodes := range graph.Tasks {
		DebugOutput("Starting execution level %d\n", executionLevel)
		var tasks []Task
		for _, node := range nodes {
			tasks = append(tasks, getTasks(node)...)
		}
		// Run all tasks of this level on all hosts in parallel, regardless of errors
		numExpectedResults := len(tasks) * len(contexts)
		ch := make(chan TaskResult, numExpectedResults)
		var wg sync.WaitGroup

		executedOnHost = append(executedOnHost, make(map[string][]Task))

		for _, task := range tasks {
			for _, c := range contexts {
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
			fmt.Printf("[%s - %s]:execute\n", c.Host, task.Name)
			c.History[task.Name] = result.Output
			if task.Register != "" {
				c.Facts[task.Register] = OutputToFacts(result.Output)
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
