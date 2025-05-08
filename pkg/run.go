package pkg

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"

	"github.com/AlexanderGrooff/spage/pkg/config"
)

// RegisterVariableIfNeeded checks if a task result should be registered as a variable
// and stores it in the HostContext's Facts if necessary.
func RegisterVariableIfNeeded(result TaskResult, task Task, c *Closure) {
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

// LocalTaskRunner implements the TaskRunner interface for local execution.
// It directly calls the task's ExecuteModule method.
type LocalTaskRunner struct{}

// RunTask executes a task locally.
// It directly calls task.ExecuteModule and returns its result.
// The TaskResult from ExecuteModule is expected to be populated by handleResult (called within ExecuteModule).
func (r *LocalTaskRunner) RunTask(ctx context.Context, task Task, closure *Closure, cfg *config.Config) TaskResult {
	// Check for context cancellation before execution if the task execution itself is long
	// and doesn't frequently check context. However, task.ExecuteModule should ideally handle this.
	select {
	case <-ctx.Done():
		common.LogWarn("Context cancelled before local task execution", map[string]interface{}{
			"task": task.Name, "host": closure.HostContext.Host.Name, "error": ctx.Err(),
		})
		return TaskResult{
			Task:     task,
			Closure:  closure,
			Error:    fmt.Errorf("task %s on host %s cancelled before local execution: %w", task.Name, closure.HostContext.Host.Name, ctx.Err()),
			Status:   TaskStatusFailed, // Or a dedicated "cancelled" status
			Failed:   true,
			Duration: 0, // Task didn't run
		}
	default:
	}

	// Task.ExecuteModule is responsible for:
	// 1. Running the module.
	// 2. Calling handleResult, which:
	//    a. Sets TaskResult.Status, Failed, Changed.
	//    b. Calls registerVariableIfNeeded (updates closure.HostContext.Facts).
	//    c. Calls setTaskStatus (updates closure.HostContext.Facts for register var).
	//    d. Handles FailedWhen, ChangedWhen, IgnoreErrors.
	result := task.ExecuteModule(closure)

	// Ensure Task and Closure are set in the result, as ExecuteModule might not always do this
	// (though it should, via the TaskResult it initializes).
	result.Task = task
	result.Closure = closure

	return result
}

// ExecuteWithTimeout wraps ExecuteWithContext with a timeout.
func ExecuteWithTimeout(cfg *config.Config, graph Graph, inventoryFile string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// Pass the already configured cfg directly
	return ExecuteWithContext(ctx, cfg, graph, inventoryFile)
}

// ExecuteWithContext now uses the BaseExecutor for its core logic.
func ExecuteWithContext(ctx context.Context, cfg *config.Config, graph Graph, inventoryFile string) error {
	localRunner := &LocalTaskRunner{}
	// NewBaseExecutor is in pkg/executor.go (which should be in the same package 'pkg')
	executor := NewBaseExecutor(localRunner)

	// The error returned by executor.Execute will be the overall status of the play.
	err := executor.Execute(ctx, cfg, graph, inventoryFile)
	if err != nil {
		// Log the final error from BaseExecutor if it's not just a run failure message
		if !strings.Contains(err.Error(), "run failed") && !strings.Contains(err.Error(), "execution cancelled") {
			common.LogError("Play execution failed with critical error", map[string]interface{}{"error": err.Error()})
		}
		return err // Propagate the error (e.g., "run failed and tasks reverted", or a setup error)
	}
	return nil
}

// Execute executes the graph using the default background context and config.
func Execute(cfg *config.Config, graph Graph, inventoryFile string) error {
	// Call ExecuteWithContext, which now uses the BaseExecutor.
	err := ExecuteWithContext(context.Background(), cfg, graph, inventoryFile)
	// Debug log for the completion of the Execute call itself.
	// The BaseExecutor handles its own detailed logging.
	common.DebugOutput("Local execution (Execute function) completed.", map[string]interface{}{"error": err})
	return err
}

// ConvertOutputToFactsMap remains in run.go as it's used by task.go's registerVariableIfNeeded,
// which is still part of the core task execution logic called by modules.
// It's a utility for converting module output to facts, not directly tied to executor orchestration.
// Get the reflect.Type of the ModuleOutput interface once.
var moduleOutputType = reflect.TypeOf((*ModuleOutput)(nil)).Elem()

func ConvertOutputToFactsMap(output ModuleOutput) interface{} {
	if output == nil {
		return nil
	}

	outputValue := reflect.ValueOf(output)
	outputType := outputValue.Type()

	if outputType.Kind() == reflect.Ptr {
		if outputValue.IsNil() {
			return nil
		}
		outputValue = outputValue.Elem()
		outputType = outputValue.Type()
	}

	if outputValue.IsValid() && outputType.Kind() == reflect.Struct {
		factsMap := make(map[string]interface{})
		for i := 0; i < outputValue.NumField(); i++ {
			fieldValue := outputValue.Field(i)
			typeField := outputType.Field(i)

			if typeField.IsExported() {
				if typeField.Type == moduleOutputType {
					continue
				}
				key := strings.ToLower(typeField.Name)
				fieldInterface := fieldValue.Interface()
				fieldValType := reflect.TypeOf(fieldInterface)
				fieldValKind := fieldValType.Kind()
				if fieldValKind == reflect.Ptr {
					fieldValKind = fieldValType.Elem().Kind()
				}

				if fieldValKind == reflect.Struct {
					factsMap[key] = convertInterfaceToMapRecursive(fieldInterface)
				} else {
					factsMap[key] = fieldInterface
				}
			}
		}
		if changedMethod := outputValue.MethodByName("Changed"); changedMethod.IsValid() {
			results := changedMethod.Call(nil)
			if len(results) > 0 && results[0].Kind() == reflect.Bool {
				factsMap["changed"] = results[0].Bool()
			}
		} else if outputValue.CanAddr() {
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
	return output
}

func convertInterfaceToMapRecursive(data interface{}) interface{} {
	if data == nil {
		return nil
	}

	value := reflect.ValueOf(data)
	typeInfo := value.Type()

	if typeInfo.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
		typeInfo = value.Type()
	}

	if typeInfo.Kind() != reflect.Struct {
		return data
	}

	mapResult := make(map[string]interface{})
	for i := 0; i < value.NumField(); i++ {
		fieldValue := value.Field(i)
		typeField := typeInfo.Field(i)

		if typeField.IsExported() {
			key := strings.ToLower(typeField.Name)
			mapResult[key] = convertInterfaceToMapRecursive(fieldValue.Interface())
		}
	}
	return mapResult
}

// func RevertTasksWithConfig(executedTasks []map[string]chan Task, contexts map[string]*HostContext, cfg *config.Config) error {
// 	// Revert all tasks per level in descending order
// 	recapStats := make(map[string]map[string]int) // Track revert stats too
// 	for hostname := range contexts {
// 		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0}
// 	}

// 	for executionLevel := len(executedTasks) - 1; executionLevel >= 0; executionLevel-- {
// 		common.DebugOutput("Reverting level %d", executionLevel)
// 		for hostname, taskHistoryCh := range executedTasks[executionLevel] {
// 			// TODO: revert hosts in parallel per executionlevel
// 			common.DebugOutput("Reverting host %s on level %d", hostname, executionLevel)
// 			for task := range taskHistoryCh {
// 				// Conditionally print REVERT TASK header
// 				if cfg.Logging.Format == "plain" {
// 					fmt.Printf("\nREVERT TASK [%s] *****************************************\n", task.Name)
// 				}
// 				c, ok := contexts[hostname]
// 				logData := map[string]interface{}{ // Prepare structured log data for revert
// 					"host":   hostname,
// 					"task":   task.Name,
// 					"action": "revert",
// 				}

// 				if !ok {
// 					logData["status"] = "failed"
// 					logData["error"] = "context not found"
// 					recapStats[hostname]["failed"]++
// 					if cfg.Logging.Format == "plain" {
// 						fmt.Printf("failed: [%s] => (context not found)\n", hostname)
// 					} else {
// 						common.LogError("Revert task failed", logData)
// 					}
// 					continue // Skip this task revert
// 				}

// 				// Check if the task actually has a revert command defined using the interface method
// 				if !task.Params.HasRevert() {
// 					// Task doesn't have a revert command, log as 'ok' (no action needed)
// 					logData["status"] = "ok"
// 					logData["changed"] = false
// 					logData["message"] = "No revert command defined for this task"
// 					recapStats[hostname]["ok"]++
// 					if cfg.Logging.Format == "plain" {
// 						fmt.Printf("ok: [%s] => (no revert defined)\n", hostname)
// 					} else {
// 						common.LogInfo("Skipping revert task", logData)
// 					}
// 					continue // Move to the next task
// 				}

// 				// Proceed with revert only if HasRevert() is true
// 				closure := ConstructClosure(c, task)
// 				tOutput := task.RevertModule(closure)
// 				if tOutput.Error != nil {
// 					logData["status"] = "failed"
// 					logData["error"] = tOutput.Error.Error()
// 					if tOutput.Output != nil {
// 						logData["output"] = tOutput.Output.String()
// 					}
// 					recapStats[hostname]["failed"]++
// 					if cfg.Logging.Format == "plain" {
// 						fmt.Printf("failed: [%s] => (%v)\n", hostname, tOutput.Error)
// 					} else {
// 						common.LogError("Revert task failed", logData)
// 					}
// 					// Decide if one revert failure should stop everything
// 				} else if tOutput.Output != nil && tOutput.Output.Changed() {
// 					logData["status"] = "changed"
// 					logData["changed"] = true
// 					logData["output"] = tOutput.Output.String()
// 					recapStats[hostname]["changed"]++
// 					if cfg.Logging.Format == "plain" {
// 						fmt.Printf("changed: [%s]\n", hostname)
// 					} else {
// 						common.LogInfo("Revert task changed", logData)
// 					}
// 				} else {
// 					logData["status"] = "ok"
// 					logData["changed"] = false
// 					if tOutput.Output != nil {
// 						logData["output"] = tOutput.Output.String()
// 					}
// 					recapStats[hostname]["ok"]++
// 					if cfg.Logging.Format == "plain" {
// 						fmt.Printf("ok: [%s]\n", hostname)
// 					} else {
// 						common.LogInfo("Revert task ok", logData)
// 					}
// 				}
// 			}
// 			common.DebugOutput("Reverted level %d for host %s", executionLevel, hostname)
// 		}
// 	}

// 	// Conditionally print REVERT RECAP
// 	if cfg.Logging.Format == "plain" {
// 		fmt.Printf("\nREVERT RECAP ****************************************************\n")
// 		// Print actual revert recap stats
// 		for hostname, stats := range recapStats {
// 			fmt.Printf("%s : ok=%d    changed=%d    failed=%d\n",
// 				hostname, stats["ok"], stats["changed"], stats["failed"])
// 		}
// 	} else {
// 		common.LogInfo("Revert recap", map[string]interface{}{"stats": recapStats})
// 	}

// 	// Check if any revert failed
// 	for _, stats := range recapStats {
// 		if stats["failed"] > 0 {
// 			return fmt.Errorf("one or more tasks failed to revert")
// 		}
// 	}
// 	common.DebugOutput("Revert done")

// 	return nil
// }
