package pkg

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"

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
	playTarget := "localhost" // Default play target name
	if inventoryFile == "" {
		common.LogDebug("No inventory file specified", map[string]interface{}{
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
		playTarget = inventoryFile // Use inventory file name for play target
		inventory, err = LoadInventory(inventoryFile)
		if err != nil {
			return fmt.Errorf("failed to load inventory: %w", err)
		}
	}

	if err := graph.CheckInventoryForRequiredInputs(inventory); err != nil {
		return fmt.Errorf("failed to check inventory for required inputs: %w", err)
	}
	contexts, err := inventory.GetContextForRun()
	if err != nil {
		return fmt.Errorf("failed to get contexts for run: %w", err)
	}

	var hostTaskLevelHistory []map[string]chan Task
	fmt.Printf("\nPLAY [%s] ****************************************************\n", playTarget)

	// Initialize recap statistics
	recapStats := make(map[string]map[string]int)
	for hostname := range contexts {
		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0, "skipped": 0}
	}

	for executionLevel, nodes := range graph.Tasks {
		select {
		case <-ctx.Done():
			return fmt.Errorf("execution cancelled: %w", ctx.Err())
		default:
		}

		var tasks []Task
		for _, node := range nodes {
			tasks = append(tasks, getTasks(node)...)
		}

		numExpectedResults := len(tasks) * len(contexts)
		resultsCh := make(chan TaskResult, numExpectedResults)
		errCh := make(chan error, 1)

		// Initialize history for this level
		hostTaskLevelHistory = append(hostTaskLevelHistory, make(map[string](chan Task)))
		for hostname := range contexts {
			hostTaskLevelHistory[executionLevel][hostname] = make(chan Task, len(tasks))
			common.DebugOutput("Expecting %d results for %s on level %d", len(tasks), hostname, executionLevel)
		}

		common.DebugOutput("Scheduling %d tasks on level %d", len(tasks), executionLevel)
		if cfg.ExecutionMode == "parallel" {
			go loadLevelParallel(ctx, tasks, contexts, resultsCh, errCh)
		} else {
			go loadLevelSequential(ctx, tasks, contexts, resultsCh, errCh)
		}

		// Process results
		common.DebugOutput("Executing tasks on level %d", executionLevel)
		var errored bool
		resultCount := 0
		processedTasks := make(map[string]bool) // Track processed tasks for parallel header printing

		for result := range resultsCh {
			resultCount++
			hostname := result.Context.Host.Name
			task := result.Task
			c := result.Context
			duration := result.Duration

			fmt.Printf("\nTASK [%s] ****************************************************\n", task.Name)
			c.History.Store(task.Name, result.Output)
			if task.Register != "" {
				// Attempt to convert the output to a map with lowercase keys
				valueToStore := convertOutputToFactsMap(result.Output)

				// Check if conversion failed (i.e., it's not a map and not nil)
				if _, isMap := valueToStore.(map[string]interface{}); !isMap && valueToStore != nil {
					common.LogWarn("Storing non-struct/non-convertible ModuleOutput for registered variable; structure may not be accessible as expected in templates", map[string]interface{}{
						"task":     result.Task.Name,
						"host":     result.Context.Host.Name,
						"variable": result.Task.Register,
						"type":     fmt.Sprintf("%T", result.Output),
					})
					// Keep the original value in this case (it was already assigned to valueToStore by convertOutputToFactsMap)
				}

				// Only store if valueToStore is not nil
				if valueToStore != nil {
					common.LogDebug("Registering variable", map[string]interface{}{ // Use valueToStore in log
						"task":     result.Task.Name,
						"host":     result.Context.Host.Name,
						"variable": result.Task.Register,
						"value":    valueToStore,
					})
					c.Facts.Store(task.Register, valueToStore)
				}
			}
			hostTaskLevelHistory[executionLevel][hostname] <- task

			// Prepare structured log data
			logData := map[string]any{
				"host":     hostname,
				"task":     task.Name,
				"duration": duration.String(),
			}

			if result.Error != nil {
				logData["status"] = "failed"
				logData["error"] = result.Error.Error()
				if result.Output != nil { // Include output details even on failure if available
					logData["output"] = result.Output.String()
				}
				recapStats[hostname]["failed"]++
				if cfg.Logging.Format == "plain" {
					fmt.Printf("failed: [%s] => (%v)\n", hostname, result.Error)
					PPrintOutput(result.Output, result.Error) // Print details on error for plain format
				} else {
					common.LogError("Task failed", logData)
				}
				common.DebugOutput("error executing '%s': %v\n\nREVERTING\n\n", task, result.Error)
				errored = true
			} else if result.Output != nil && result.Output.Changed() {
				logData["status"] = "changed"
				logData["changed"] = true
				logData["output"] = result.Output.String()
				recapStats[hostname]["changed"]++
				if cfg.Logging.Format == "plain" {
					fmt.Printf("changed: [%s] => \n%v\n", hostname, result.Output)
				} else {
					common.LogInfo("Task changed", logData)
				}
			} else if result.Output == nil && result.Error == nil { // Skipped task
				logData["status"] = "skipped"
				recapStats[hostname]["skipped"]++
				if cfg.Logging.Format == "plain" {
					fmt.Printf("skipped: [%s]\n", hostname)
				} else {
					common.LogInfo("Task skipped", logData)
				}
			} else { // OK task
				logData["status"] = "ok"
				logData["changed"] = false
				if result.Output != nil {
					logData["output"] = result.Output.String()
				}
				recapStats[hostname]["ok"]++
				if cfg.Logging.Format == "plain" {
					fmt.Printf("ok: [%s]\n", hostname)
				} else {
					common.LogInfo("Task ok", logData)
				}
			}

			processedTasks[task.Name] = true // Mark task as processed for this level
		}
		for hostname := range contexts {
			common.DebugOutput("Closing history channel for %s on level %d", hostname, executionLevel)
			close(hostTaskLevelHistory[executionLevel][hostname])
		}

		// Check for context cancellation errors that might have occurred
		select {
		case err := <-errCh:
			return fmt.Errorf("execution cancelled: %w", err)
		default:
			// No error from errCh
		}

		if errored {
			if cfg.Logging.Format == "plain" {
				fmt.Printf("\nREVERTING TASKS **********************************************\n")
			}
			if err := RevertTasksWithConfig(hostTaskLevelHistory, contexts, cfg); err != nil {
				return fmt.Errorf("run failed during revert: %w", err)
			}
			return fmt.Errorf("run failed and tasks reverted")
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

	// Conditionally print PLAY RECAP
	if cfg.Logging.Format == "plain" {
		fmt.Printf("\nPLAY RECAP ****************************************************\n")
		// Print actual recap stats
		for hostname, stats := range recapStats {
			// Basic formatting, adjust spacing as needed
			fmt.Printf("%s : ok=%d    changed=%d    failed=%d    skipped=%d\n",
				hostname, stats["ok"], stats["changed"], stats["failed"], stats["skipped"])
		}
	} else {
		// Log final recap stats as structured data
		common.LogInfo("Play recap", map[string]interface{}{"stats": recapStats})
	}

	return nil
}

func loadLevelSequential(ctx context.Context, tasks []Task, contexts map[string]*HostContext, resultsCh chan TaskResult, errCh chan error) {
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
}

func loadLevelParallel(ctx context.Context, tasks []Task, contexts map[string]*HostContext, resultsCh chan TaskResult, errCh chan error) {
	var wg sync.WaitGroup

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
}

// Get the reflect.Type of the ModuleOutput interface once.
var moduleOutputType = reflect.TypeOf((*ModuleOutput)(nil)).Elem()

// convertOutputToFactsMap attempts to convert a ModuleOutput struct
// into a map[string]interface{} using reflection, creating lowercase keys
// for exported fields to mimic Ansible fact registration.
// It also adds a 'changed' key based on the output's Changed() method.
// If the input is not a struct or is nil, it returns the original value.
func convertOutputToFactsMap(output ModuleOutput) interface{} {
	if output == nil {
		return nil
	}

	outputValue := reflect.ValueOf(output)
	outputType := outputValue.Type()

	// Handle pointers if the output value itself is a pointer
	if outputType.Kind() == reflect.Ptr {
		outputValue = outputValue.Elem()
		outputType = outputValue.Type()
	}

	// Ensure we have a valid struct to reflect on
	if outputValue.IsValid() && outputType.Kind() == reflect.Struct {
		factsMap := make(map[string]interface{})
		for i := 0; i < outputValue.NumField(); i++ {
			fieldValue := outputValue.Field(i)
			typeField := outputType.Field(i)

			// Only include exported fields
			if typeField.IsExported() {
				// Skip the embedded ModuleOutput interface field itself
				if typeField.Type == moduleOutputType {
					continue
				}
				// Use lowercase field name as key
				key := strings.ToLower(typeField.Name)
				factsMap[key] = fieldValue.Interface()
			}
		}
		// Add common 'changed' field
		factsMap["changed"] = output.Changed()
		return factsMap
	}

	// If not a struct, return the original value
	return output
}

// Execute executes the graph using the default background context
func Execute(cfg *config.Config, graph Graph, inventoryFile string) error {
	err := ExecuteWithContext(context.Background(), cfg, graph, inventoryFile)
	common.DebugOutput("Execute done: %v", err)
	return err
}

func RevertTasksWithConfig(executedTasks []map[string]chan Task, contexts map[string]*HostContext, cfg *config.Config) error {
	// Revert all tasks per level in descending order
	recapStats := make(map[string]map[string]int) // Track revert stats too
	for hostname := range contexts {
		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0}
	}

	for executionLevel := len(executedTasks) - 1; executionLevel >= 0; executionLevel-- {
		common.DebugOutput("Reverting level %d", executionLevel)
		for hostname, taskHistoryCh := range executedTasks[executionLevel] {
			// TODO: revert hosts in parallel per executionlevel
			common.DebugOutput("Reverting host %s on level %d", hostname, executionLevel)
			for task := range taskHistoryCh {
				// Conditionally print REVERT TASK header
				if cfg.Logging.Format == "plain" {
					fmt.Printf("\nREVERT TASK [%s] *****************************************\n", task.Name)
				}
				c, ok := contexts[hostname]
				logData := map[string]interface{}{ // Prepare structured log data for revert
					"host":   hostname,
					"task":   task.Name,
					"action": "revert",
				}

				if !ok {
					logData["status"] = "failed"
					logData["error"] = "context not found"
					recapStats[hostname]["failed"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("failed: [%s] => (context not found)\n", hostname)
					} else {
						common.LogError("Revert task failed", logData)
					}
					continue // Skip this task revert
				}
				tOutput := task.RevertModule(c)
				if tOutput.Error != nil {
					logData["status"] = "failed"
					logData["error"] = tOutput.Error.Error()
					if tOutput.Output != nil {
						logData["output"] = tOutput.Output.String()
					}
					recapStats[hostname]["failed"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("failed: [%s] => (%v)\n", hostname, tOutput.Error)
					} else {
						common.LogError("Revert task failed", logData)
					}
					// Decide if one revert failure should stop everything
				} else if tOutput.Output != nil && tOutput.Output.Changed() {
					logData["status"] = "changed"
					logData["changed"] = true
					logData["output"] = tOutput.Output.String()
					recapStats[hostname]["changed"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("changed: [%s]\n", hostname)
					} else {
						common.LogInfo("Revert task changed", logData)
					}
				} else {
					logData["status"] = "ok"
					logData["changed"] = false
					if tOutput.Output != nil {
						logData["output"] = tOutput.Output.String()
					}
					recapStats[hostname]["ok"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("ok: [%s]\n", hostname)
					} else {
						common.LogInfo("Revert task ok", logData)
					}
				}
			}
			common.DebugOutput("Reverted level %d for host %s", executionLevel, hostname)
		}
	}

	// Conditionally print REVERT RECAP
	if cfg.Logging.Format == "plain" {
		fmt.Printf("\nREVERT RECAP ****************************************************\n")
		// Print actual revert recap stats
		for hostname, stats := range recapStats {
			fmt.Printf("%s : ok=%d    changed=%d    failed=%d\n",
				hostname, stats["ok"], stats["changed"], stats["failed"])
		}
	} else {
		common.LogInfo("Revert recap", map[string]interface{}{"stats": recapStats})
	}

	// Check if any revert failed
	for _, stats := range recapStats {
		if stats["failed"] > 0 {
			return fmt.Errorf("one or more tasks failed to revert")
		}
	}
	common.DebugOutput("Revert done")

	return nil
}
