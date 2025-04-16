package pkg

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/config"
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

// ExecuteWithTimeout wraps Execute with a timeout
func ExecuteWithTimeout(cfg *config.Config, graph Graph, inventoryFile string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return ExecuteWithContext(ctx, cfg, graph, inventoryFile)
}

// ExecuteWithContext executes the graph with context control
func ExecuteWithContext(ctx context.Context, cfg *config.Config, graph Graph, inventoryFile string) error {
	var inventory *Inventory
	var err error
	if inventoryFile == "" {
		LogInfo("No inventory file specified", map[string]interface{}{
			"message": "Assuming target is this machine",
		})
		inventory = &Inventory{
			Hosts: map[string]*Host{
				"localhost": {
					Name:    "localhost",
					IsLocal: true,
					Host:    "localhost",
				},
			},
		}
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
		select {
		case <-ctx.Done():
			return fmt.Errorf("execution cancelled: %w", ctx.Err())
		default:
		}

		DebugOutput("Starting execution level %d\n", executionLevel)
		var tasks []Task
		for _, node := range nodes {
			tasks = append(tasks, getTasks(node)...)
		}

		numExpectedResults := len(tasks) * len(contexts)
		resultsCh := make(chan TaskResult, numExpectedResults)
		errCh := make(chan error, 1)
		var wg sync.WaitGroup

		executedOnHost = append(executedOnHost, make(map[string][]Task))

		if cfg.ExecutionMode == "parallel" {
			for _, task := range tasks {
				for _, c := range contexts {
					wg.Add(1)
					go func(task Task, c *HostContext) {
						defer wg.Done()
						select {
						case <-ctx.Done():
							errCh <- ctx.Err()
							return
						default:
							result := task.ExecuteModule(c)
							resultsCh <- result
						}
					}(task, c)
				}
			}

			// Wait for completion or context cancellation in parallel mode
			go func() {
				wg.Wait()
				close(resultsCh)
			}()
		} else { // sequential execution
			go func() {
				defer close(resultsCh)
				for _, task := range tasks {
					for _, c := range contexts {
						select {
						case <-ctx.Done():
							errCh <- ctx.Err()
							return
						default:
							result := task.ExecuteModule(c)
							resultsCh <- result
							// If a task fails in sequential mode, stop this level
							if result.Error != nil {
								return
							}
						}
					}
				}
			}()
		}

		// Process results
		var errored bool
		resultCount := 0
		for result := range resultsCh {
			resultCount++
			hostname := result.Context.Host.Name
			task := result.Task
			c := result.Context
			LogInfo("Executing task", map[string]interface{}{
				"host": c.Host,
				"task": task.Name,
			})
			c.History[task.Name] = result.Output
			if task.Register != "" {
				c.Facts[task.Register] = OutputToFacts(result.Output)
			}
			executedOnHost[executionLevel][hostname] = append(executedOnHost[executionLevel][hostname], task)
			PPrintOutput(result.Output, result.Error)

			if result.Error != nil {
				DebugOutput("error executing '%s': %v\n\nREVERTING\n\n", task, result.Error)
				errored = true
				if cfg.ExecutionMode == "sequential" {
					// Drain remaining results if any (shouldn't be many in sequential error case)
					go func() {
						for range resultsCh {
						}
					}()
					break // Stop processing results for this level
				}
			}
		}

		// Check for context cancellation errors that might have occurred
		select {
		case err := <-errCh:
			return fmt.Errorf("execution cancelled: %w", err)
		default:
			// No error from errCh
		}

		if errored {
			if err := RevertTasks(executedOnHost, contexts); err != nil {
				return fmt.Errorf("run failed: %w", err)
			}
			return fmt.Errorf("reverted all tasks")
		}

		// Ensure all expected results were processed (especially important for parallel mode)
		if cfg.ExecutionMode == "parallel" && resultCount != numExpectedResults {
			// This might indicate an issue like premature channel closing or context cancellation
			// Check context error again for clarity
			select {
			case <-ctx.Done():
				return fmt.Errorf("execution cancelled during result processing: %w", ctx.Err())
			default:
				return fmt.Errorf("internal error: expected %d results, got %d", numExpectedResults, resultCount)
			}
		}
	}

	DebugOutput("All tasks executed successfully")
	return nil
}

// Original Execute function now needs the config
func Execute(cfg *config.Config, graph Graph, inventoryFile string) error {
	return ExecuteWithContext(context.Background(), cfg, graph, inventoryFile)
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
