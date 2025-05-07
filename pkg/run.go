package pkg

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"

	"github.com/AlexanderGrooff/spage/pkg/config"
)

// registerVariableIfNeeded checks if a task result should be registered as a variable
// and stores it in the HostContext's Facts if necessary.
func registerVariableIfNeeded(result TaskResult, task Task, c *Closure) {
	// Only register if the task has a name assigned to the 'register' key
	if task.Register == "" {
		return
	}

	var valueToStore interface{}
	var ignoredErr *IgnoredTaskError

	// Check if the error is an IgnoredTaskError or just a regular error
	if errors.As(result.Error, &ignoredErr) {
		// It's an ignored error
		originalErr := ignoredErr.Unwrap()    // Get the original error
		failureMap := map[string]interface{}{ // Register failure details
			"failed":  true,
			"changed": false,
			"msg":     originalErr.Error(),
			"ignored": true, // Add an explicit ignored flag
		}
		// Include output facts if available
		if result.Output != nil {
			if factProvider, ok := result.Output.(FactProvider); ok {
				outputFacts := factProvider.AsFacts()
				for k, v := range outputFacts {
					failureMap[k] = v
				}
			}
		}
		valueToStore = failureMap
		common.LogDebug("Ignored error", map[string]interface{}{
			"task":  task.Name,
			"host":  c.HostContext.Host.Name,
			"error": originalErr.Error(),
			"value": valueToStore,
		})
	} else if result.Error != nil {
		// It's a regular, non-ignored error
		failureMap := map[string]interface{}{ // Register failure details
			"failed":  true,
			"changed": false,
			"msg":     result.Error.Error(),
		}
		// Include output facts if available
		if result.Output != nil {
			if factProvider, ok := result.Output.(FactProvider); ok {
				outputFacts := factProvider.AsFacts()
				for k, v := range outputFacts {
					failureMap[k] = v
				}
			}
		}
		valueToStore = failureMap
	} else if result.Output != nil {
		// If successful and output exists, register the output facts
		valueToStore = ConvertOutputToFactsMap(result.Output)
	} else {
		// If successful but no output (e.g., skipped task), register a minimal success map
		valueToStore = map[string]interface{}{ // Ensure something is registered for skipped/ok tasks
			"failed":  false,
			"changed": false,
			"skipped": result.Output == nil, // Mark as skipped if output is nil
			"ignored": false,                // Explicitly false for non-ignored cases
		}
	}

	if valueToStore != nil {
		common.LogDebug("Registering variable", map[string]interface{}{
			"task":     task.Name,
			"host":     c.HostContext.Host.Name,
			"variable": task.Register,
			"value":    valueToStore, // Log the actual map being stored
		})
		c.HostContext.Facts.Store(task.Register, valueToStore)
	}
}

func setTaskStatus(result TaskResult, task Task, c *Closure) {
	if task.Register == "" {
		return
	}
	facts, _ := c.GetFact(task.Register)
	if facts == nil {
		facts = map[string]interface{}{
			"failed":  result.Failed,
			"changed": result.Changed,
		}
	} else {
		if factsMap, ok := facts.(map[string]interface{}); ok {
			factsMap["failed"] = result.Failed
			factsMap["changed"] = result.Changed
			facts = factsMap // Assign back the modified map
		} else {
			// Handle cases where the loaded value is not a map[string]interface{}
			// For now, let's overwrite with a new map, but you might want different logic.
			facts = map[string]interface{}{
				"failed":  result.Failed,
				"changed": result.Changed,
			}
		}
	}
	c.HostContext.Facts.Store(task.Register, facts)
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
	defer func() {
		for _, c := range contexts {
			c.Close()
		}
	}()

	var hostTaskLevelHistory []map[string]chan Task
	fmt.Printf("\nPLAY [%s] ****************************************************\n", playTarget)

	// Initialize recap statistics
	recapStats := make(map[string]map[string]int)
	for hostname := range contexts {
		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0, "skipped": 0}
	}

	var orderedGraph [][]Task
	if cfg.ExecutionMode == "parallel" {
		orderedGraph = graph.ParallelTasks()
	} else {
		orderedGraph = graph.SequentialTasks()
	}
	for executionLevel, tasks := range orderedGraph {
		select {
		case <-ctx.Done():
			return fmt.Errorf("execution cancelled: %w", ctx.Err())
		default:
		}

		// Initialize history for this level
		hostTaskLevelHistory = append(hostTaskLevelHistory, make(map[string](chan Task)))
		numExpectedResults := 0
		for hostname := range contexts {
			numTasks := 0
			for _, task := range tasks {
				closures, err := getTaskClosures(task, contexts[hostname])
				if err != nil {
					return fmt.Errorf("failed to get task closures: %w", err)
				}
				nrClosures := len(closures)
				numTasks += nrClosures
				common.DebugOutput("Expecting %d results for task %s on level %d", nrClosures, task.Name, executionLevel)
			}
			numExpectedResults += numTasks
			hostTaskLevelHistory[executionLevel][hostname] = make(chan Task, numTasks)
			common.DebugOutput("Expecting %d results for %s on level %d", numTasks, hostname, executionLevel)
		}
		resultsCh := make(chan TaskResult, numExpectedResults)
		errCh := make(chan error, 1)

		common.DebugOutput("Scheduling %d tasks on level %d", numExpectedResults, executionLevel)
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
			hostname := result.Closure.HostContext.Host.Name
			task := result.Task
			c := result.Closure
			duration := result.Duration

			c.HostContext.History.Store(task.Name, result.Output)

			// TODO: how to handle this when there's a loop of tasks?
			hostTaskLevelHistory[executionLevel][hostname] <- task

			// Prepare structured log data
			logData := map[string]any{
				"host":     hostname,
				"task":     task.Name,
				"duration": duration.String(),
				"status":   result.Status,
			}
			recapStats[hostname][result.Status.String()]++

			var ignoredErr *IgnoredTaskError
			if errors.As(result.Error, &ignoredErr) { // Check specifically for IgnoredTaskError
				// Task failed but error was ignored
				originalErr := ignoredErr.Unwrap()
				logData["ignored"] = true
				logData["error"] = originalErr.Error() // Use the original error message
				if result.Output != nil {              // Include output details if available
					logData["output"] = result.Output.String()
				}
				if cfg.Logging.Format == "plain" {
					fmt.Printf("failed: [%s] => (ignored error: %v)\n", hostname, originalErr)
					PPrintOutput(result.Output, originalErr)
				} else {
					common.LogWarn("Task failed (ignored)", logData)
				}
				// DO NOT set errored = true here
			} else if result.Error != nil {
				// Genuine, non-ignored task failure
				logData["error"] = result.Error.Error()
				if result.Output != nil { // Include output details even on failure if available
					logData["output"] = result.Output.String()
				}
				if cfg.Logging.Format == "plain" {
					fmt.Printf("failed: [%s] => (%v)\n", hostname, result.Error)
					PPrintOutput(result.Output, result.Error) // Print details on error for plain format
				} else {
					common.LogError("Task failed", logData)
				}
				common.DebugOutput("error executing '%s': %v\n\nREVERTING\n\n", task, result.Error)
				errored = true // This task failure triggers revert
			} else if result.Output != nil && result.Output.Changed() {
				logData["changed"] = true
				logData["output"] = result.Output.String()
				if cfg.Logging.Format == "plain" {
					fmt.Printf("changed: [%s] => \n%v\n", hostname, result.Output)
				} else {
					common.LogInfo("Task changed", logData)
				}
			} else if result.Output == nil && result.Error == nil { // Skipped task (Error is nil, and not an IgnoredTaskError)
				recapStats[hostname]["skipped"]++
				if cfg.Logging.Format == "plain" {
					fmt.Printf("skipped: [%s]\n", hostname)
				} else {
					common.LogInfo("Task skipped", logData)
				}
			} else { // OK task (Error is nil, not IgnoredTaskError, and Output exists but not changed)
				logData["changed"] = false
				if result.Output != nil {
					logData["output"] = result.Output.String()
				}
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

func startTask(task Task, closure *Closure, resultsCh chan TaskResult, wg *sync.WaitGroup) TaskResult {
	if wg != nil {
		defer wg.Done()
	}
	fmt.Printf("\nTASK [%s] ****************************************************\n", task.Name)
	result := task.ExecuteModule(closure)
	resultsCh <- result
	return result
}

func getTaskClosures(task Task, c *HostContext) ([]*Closure, error) {
	if task.Loop == nil {
		closure := ConstructClosure(c, task)
		return []*Closure{closure}, nil
	} else {
		return getLoopClosures(task, c)
	}
}

func ParseLoop(task Task, c *HostContext) ([]interface{}, error) {
	var loopItems []interface{}          // Use interface{} to hold actual items (string, map, etc.)
	closure := ConstructClosure(c, task) // Base closure for potential lookups

	switch loopValue := task.Loop.(type) {
	case string:
		trimmedLoopStr := strings.TrimSpace(loopValue)
		// Check if it looks like a simple variable template {{ var }}
		if strings.HasPrefix(trimmedLoopStr, "{{") && strings.HasSuffix(trimmedLoopStr, "}}") {
			varName := strings.TrimSpace(trimmedLoopStr[2 : len(trimmedLoopStr)-2])
			// Attempt to directly look up the variable in the facts
			factValue, found := c.Facts.Load(varName) // Use Facts.Load instead of GetFact
			if found {
				// Check if the retrieved fact is a slice
				val := reflect.ValueOf(factValue)
				if val.Kind() == reflect.Slice {
					// Convert slice to []interface{}
					loopItems = make([]interface{}, val.Len())
					for i := 0; i < val.Len(); i++ {
						loopItems[i] = val.Index(i).Interface()
					}
					common.LogDebug("Resolved loop template via direct fact lookup", map[string]interface{}{
						"task":     task.Name,
						"template": loopValue,
						"variable": varName,
						"type":     fmt.Sprintf("%T", factValue),
						"count":    len(loopItems),
					})
				} else {
					// The fact exists but is not a slice. Fall back to templating the string.
					common.LogDebug("Loop template variable found but is not a slice, falling back to string templating", map[string]interface{}{
						"task":     task.Name,
						"template": loopValue,
						"variable": varName,
						"type":     fmt.Sprintf("%T", factValue),
					})
					// Fall through to string templating logic below
				}
			} else {
				// Variable not found in facts, fall back to templating the string.
				common.LogDebug("Loop template variable not found in facts, falling back to string templating", map[string]interface{}{
					"task":     task.Name,
					"template": loopValue,
					"variable": varName,
				})
				// Fall through to string templating logic below
			}
		}

		// If loopItems is still nil, it means direct lookup didn't work or wasn't applicable.
		// Proceed with templating the string as originally intended, potentially for complex templates or literal strings.
		if loopItems == nil {
			evalLoopStr, err := TemplateString(loopValue, closure)
			if err != nil {
				// If templating fails here, it's an error
				return nil, fmt.Errorf("failed to template loop string '%s': %w", loopValue, err)
			}
			// Assume the result is a newline-separated list of strings
			loopItems = []interface{}{}
			if evalLoopStr != "" { // Avoid creating [""] for empty strings
				for _, itemStr := range strings.Split(evalLoopStr, "\n") {
					loopItems = append(loopItems, itemStr)
				}
			}
			common.LogDebug("Resolved loop via string templating and splitting", map[string]interface{}{
				"task":         task.Name,
				"template":     loopValue,
				"resultString": evalLoopStr,
				"count":        len(loopItems),
			})
		}

	case []interface{}:
		// Handles literal lists: [item1, item2] or [{k:v1}, {k:v2}] etc.
		loopItems = loopValue
		common.LogDebug("Resolved loop via direct list", map[string]interface{}{
			"task":  task.Name,
			"type":  fmt.Sprintf("%T", loopValue),
			"count": len(loopItems),
		})

	default:
		return nil, fmt.Errorf("unsupported loop type: %T", task.Loop)
	}
	return loopItems, nil
}

func getLoopClosures(task Task, c *HostContext) ([]*Closure, error) {
	loopItems, err := ParseLoop(task, c)
	if err != nil {
		return nil, fmt.Errorf("failed to parse loop: %w", err)
	}
	closures := []*Closure{}
	for _, item := range loopItems { // Iterate over the actual items
		tClosure := ConstructClosure(c, task)
		// Store the *actual* item (string, map[string]interface{}, etc.) in ExtraFacts
		tClosure.ExtraFacts["item"] = item
		closures = append(closures, tClosure)
	}
	return closures, nil
}

func loadLevelSequential(ctx context.Context, tasks []Task, contexts map[string]*HostContext, resultsCh chan TaskResult, errCh chan error) {
	defer close(resultsCh)
	var ignoredErr *IgnoredTaskError
	for _, task := range tasks {
		for _, c := range contexts {
			closures, err := getTaskClosures(task, c)
			if err != nil {
				errCh <- err
				return
			}
			for _, closure := range closures {
				select {
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				default:
					result := startTask(task, closure, resultsCh, nil)

					// Stop sequential execution if task failed
					if result.Error != nil && !errors.As(result.Error, &ignoredErr) {
						return
					}
				}
			}
		}
	}
}

func loadLevelParallel(ctx context.Context, tasks []Task, contexts map[string]*HostContext, resultsCh chan TaskResult, errCh chan error) {
	var wg sync.WaitGroup

	for _, task := range tasks {
		for _, c := range contexts {
			closures, err := getTaskClosures(task, c)
			if err != nil {
				errCh <- err
				return
			}
			for _, closure := range closures {
				wg.Add(1)
				go func() {
					select {
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					default:
						startTask(task, closure, resultsCh, &wg)
					}
				}()
			}
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

// ConvertOutputToFactsMap attempts to convert a ModuleOutput struct
// into a map[string]interface{} using reflection, creating lowercase keys
// for exported fields to mimic Ansible fact registration.
// It also adds a 'changed' key based on the output's Changed() method.
// If the input is not a struct or is nil, it returns the original value.
func ConvertOutputToFactsMap(output ModuleOutput) interface{} {
	if output == nil {
		return nil
	}

	outputValue := reflect.ValueOf(output)
	outputType := outputValue.Type()

	// Handle pointers if the output value itself is a pointer
	if outputType.Kind() == reflect.Ptr {
		// If the pointer is nil, return nil
		if outputValue.IsNil() {
			return nil
		}
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

				// --- START RECURSIVE CONVERSION ---
				fieldInterface := fieldValue.Interface()
				// Check if the field's value is itself a struct or a pointer to a struct
				fieldValType := reflect.TypeOf(fieldInterface)
				fieldValKind := fieldValType.Kind()
				if fieldValKind == reflect.Ptr {
					// If it's a pointer, get the element type's kind
					fieldValKind = fieldValType.Elem().Kind()
				}

				if fieldValKind == reflect.Struct {
					// If it's a struct (or pointer to one), recursively convert it
					// We need to pass it as a ModuleOutput for the recursive call,
					// which isn't ideal, but works if it embeds ModuleOutput or we adapt.
					// A better approach might be a dedicated recursive map converter.
					// For now, let's try a direct recursive map conversion helper.
					factsMap[key] = convertInterfaceToMapRecursive(fieldInterface)
				} else {
					// Otherwise, store the value directly
					factsMap[key] = fieldInterface
				}
				// --- END RECURSIVE CONVERSION ---
			}
		}
		// Add common 'changed' field if the original output implemented Changed()
		if changedMethod := outputValue.MethodByName("Changed"); changedMethod.IsValid() {
			results := changedMethod.Call(nil)
			if len(results) > 0 && results[0].Kind() == reflect.Bool {
				factsMap["changed"] = results[0].Bool()
			}
		} else if outputValue.CanAddr() {
			// Try calling Changed() on a pointer if the original value was not a pointer
			addrValue := outputValue.Addr()
			if changedMethod := addrValue.MethodByName("Changed"); changedMethod.IsValid() {
				results := changedMethod.Call(nil)
				if len(results) > 0 && results[0].Kind() == reflect.Bool {
					factsMap["changed"] = results[0].Bool()
				}
			}
		}

		return factsMap
	}

	// If not a struct (or pointer to one), return the original value
	return output
}

// --- START NEW HELPER FOR RECURSION ---
// convertInterfaceToMapRecursive converts any interface{} containing a struct
// (or pointer to struct) into a map[string]interface{} recursively.
// Handles basic types directly.
func convertInterfaceToMapRecursive(data interface{}) interface{} {
	if data == nil {
		return nil
	}

	value := reflect.ValueOf(data)
	typeInfo := value.Type()

	// Handle pointers
	if typeInfo.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
		typeInfo = value.Type()
	}

	// Only convert structs
	if typeInfo.Kind() != reflect.Struct {
		// Return non-struct types as is (e.g., string, int, bool, slices, etc.)
		// Note: This won't recursively convert structs *within* slices or maps yet.
		return data
	}

	// Convert struct to map
	mapResult := make(map[string]interface{})
	for i := 0; i < value.NumField(); i++ {
		fieldValue := value.Field(i)
		typeField := typeInfo.Field(i)

		if typeField.IsExported() {
			// Use lowercase field name as key (consistent with ConvertOutputToFactsMap)
			key := strings.ToLower(typeField.Name)
			// Recursively convert the field's value
			mapResult[key] = convertInterfaceToMapRecursive(fieldValue.Interface())
		}
	}
	return mapResult
}

// --- END NEW HELPER FOR RECURSION ---

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

				// Check if the task actually has a revert command defined using the interface method
				if !task.Params.HasRevert() {
					// Task doesn't have a revert command, log as 'ok' (no action needed)
					logData["status"] = "ok"
					logData["changed"] = false
					logData["message"] = "No revert command defined for this task"
					recapStats[hostname]["ok"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("ok: [%s] => (no revert defined)\n", hostname)
					} else {
						common.LogInfo("Skipping revert task", logData)
					}
					continue // Move to the next task
				}

				// Proceed with revert only if HasRevert() is true
				closure := ConstructClosure(c, task)
				tOutput := task.RevertModule(closure)
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
