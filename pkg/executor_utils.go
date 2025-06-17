package pkg

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg/common"
	"github.com/AlexanderGrooff/spage/pkg/config"
)

// PPrintOutput prints the output of a module or an error in a readable format.
func PPrintOutput(output ModuleOutput, err error) {
	if output != nil {
		// Attempt to get a string representation. Assumes ModuleOutput implements fmt.Stringer or can be printed directly.
		// More sophisticated printing might involve checking specific fields or using a custom Marshal-like method.
		fmt.Printf("%v\n", output)
	} else if err != nil {
		// Error is usually printed by the caller context, so no duplicate print here.
		// fmt.Printf("Error: %v\n", err) // Avoid double printing of error message
	}
}

// ParseLoop parses the loop directive of a task, evaluating expressions and returning items to iterate over.
func ParseLoop(task Task, c *HostContext) ([]interface{}, error) {
	var loopItems []interface{}
	closure := ConstructClosure(c, task) // Assumes ConstructClosure is available from context.go or similar

	switch loopValue := task.Loop.(type) {
	case string:
		trimmedLoopStr := strings.TrimSpace(loopValue)
		// Check if it looks like a simple variable template {{ var }}
		if strings.HasPrefix(trimmedLoopStr, "{{") && strings.HasSuffix(trimmedLoopStr, "}}") {
			varName := strings.TrimSpace(trimmedLoopStr[2 : len(trimmedLoopStr)-2])
			// Attempt to directly look up the variable in the facts
			factValue, found := c.Facts.Load(varName) // Use Facts.Load
			if found {
				val := reflect.ValueOf(factValue)
				if val.Kind() == reflect.Slice {
					loopItems = make([]interface{}, val.Len())
					for i := 0; i < val.Len(); i++ {
						loopItems[i] = val.Index(i).Interface()
					}
					common.LogDebug("Resolved loop template via direct fact lookup", map[string]interface{}{
						"task": task.Name, "template": loopValue, "variable": varName,
						"type": fmt.Sprintf("%T", factValue), "count": len(loopItems),
					})
				} else {
					common.LogDebug("Loop template variable found but is not a slice, falling back to string templating", map[string]interface{}{
						"task": task.Name, "template": loopValue, "variable": varName,
						"type": fmt.Sprintf("%T", factValue),
					})
					// Fall through to string templating logic below
				}
			} else {
				common.LogDebug("Loop template variable not found in facts, falling back to string templating", map[string]interface{}{
					"task": task.Name, "template": loopValue, "variable": varName,
				})
				// Fall through to string templating logic below
			}
		}

		// If loopItems is still nil, proceed with templating the string.
		if loopItems == nil {
			evalLoopStr, err := TemplateString(loopValue, closure) // Assumes TemplateString is available from context.go or similar
			if err != nil {
				return nil, fmt.Errorf("failed to template loop string '%s' for task '%s': %w", loopValue, task.Name, err)
			}
			loopItems = []interface{}{}
			if evalLoopStr != "" { // Avoid creating [""] for empty strings
				for _, itemStr := range strings.Split(evalLoopStr, "\n") {
					loopItems = append(loopItems, itemStr)
				}
			}
			common.LogDebug("Resolved loop via string templating and splitting", map[string]interface{}{
				"task": task.Name, "template": loopValue,
				"resultString": evalLoopStr, "count": len(loopItems),
			})
		}

	case []interface{}:
		loopItems = loopValue
		common.LogDebug("Resolved loop via direct list", map[string]interface{}{
			"task": task.Name, "type": fmt.Sprintf("%T", loopValue), "count": len(loopItems),
		})

	default:
		return nil, fmt.Errorf("unsupported loop type '%T' for task '%s'", task.Loop, task.Name)
	}
	return loopItems, nil
}

func GetDelegatedHostContext(task Task, hostContexts map[string]*HostContext, closure *Closure) (*HostContext, error) {
	if task.DelegateTo != "" {
		// Template the delegate_to value in case it contains Jinja variables
		delegateTo, err := TemplateString(task.DelegateTo, closure)
		if err != nil {
			return nil, fmt.Errorf("failed to template delegate_to '%s': %w", task.DelegateTo, err)
		}

		// First try to find the host in the existing hostContexts
		hostContext, ok := hostContexts[delegateTo]
		if ok {
			return hostContext, nil
		}

		// If not found in inventory, check for special/implicit hosts
		if delegateTo == "localhost" {
			// Create a localhost host dynamically
			localhostHost := &Host{
				Name:    "localhost",
				Host:    "localhost",
				IsLocal: true,
				Vars:    make(map[string]interface{}),
				Groups:  make(map[string]string),
			}

			// Initialize host context for localhost
			localhostContext, err := InitializeHostContext(localhostHost)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize context for localhost: %w", err)
			}

			// Add basic localhost facts
			localhostContext.Facts.Store("inventory_hostname", "localhost")
			localhostContext.Facts.Store("ansible_hostname", "localhost")

			return localhostContext, nil
		}

		// Could extend this to handle other special hosts in the future
		// For example: 127.0.0.1, or dynamic IP addresses

		return nil, fmt.Errorf("host context for delegate_to '%s' not found", delegateTo)
	}
	return nil, nil
}

// GetTaskClosures generates one or more Closures for a task, handling loops.
func GetTaskClosures(task Task, c *HostContext) ([]*Closure, error) {
	if task.Loop == nil {
		closure := ConstructClosure(c, task) // Assumes ConstructClosure is available
		return []*Closure{closure}, nil
	}

	loopItems, err := ParseLoop(task, c)
	if err != nil {
		return nil, fmt.Errorf("failed to parse loop for task '%s' on host '%s': %w", task.Name, c.Host.Name, err)
	}

	var closures []*Closure
	for _, item := range loopItems {
		tClosure := ConstructClosure(c, task)
		// "item" is the standard variable name for loop items.
		// Ansible's loop_control.loop_var allows customization; this is not currently supported here.
		tClosure.ExtraFacts["item"] = item
		closures = append(closures, tClosure)
	}
	return closures, nil
}

// RevertTasksWithConfig orchestrates the revert process for executed tasks.
func RevertTasksWithConfig(
	executedTasks []map[string]chan Task, // History of tasks *started* per level, per host
	hostContexts map[string]*HostContext,
	cfg *config.Config,
) error {
	recapStats := make(map[string]map[string]int)
	for hostname := range hostContexts {
		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0}
	}

	for executionLevel := len(executedTasks) - 1; executionLevel >= 0; executionLevel-- {
		common.DebugOutput("Reverting level %d", executionLevel)
		levelTasksByHost := executedTasks[executionLevel]

		for hostname, taskHistoryCh := range levelTasksByHost {
			common.DebugOutput("Reverting host '%s' on level %d", hostname, executionLevel)
			hostCtx, contextExists := hostContexts[hostname]
			if !contextExists {
				common.LogError("Context not found for host during revert, skipping host", map[string]interface{}{"host": hostname, "level": executionLevel})
				// How to count this in recap? For now, it's a skip for this host's tasks on this level.
				continue
			}

			// Drain the channel for this host and level
			for task := range taskHistoryCh { // taskHistoryCh should be closed by processLevelResults
				if cfg.Logging.Format == "plain" {
					fmt.Printf("\nREVERT TASK [%s] (%s) *****************************************\n", task.Name, hostname)
				} else {
					common.LogInfo("Attempting to revert task", map[string]interface{}{"task": task.Name, "host": hostname, "level": executionLevel})
				}
				logData := map[string]interface{}{
					"host": hostname, "task": task.Name, "action": "revert",
				}

				// Check if the task's module parameters implement HasRevert
				if P, ok := task.Params.Actual.(interface{ HasRevert() bool }); !ok || !P.HasRevert() {
					logData["status"] = "ok"
					logData["changed"] = false
					logData["message"] = "No revert defined for module or params"
					recapStats[hostname]["ok"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("ok: [%s] => (no revert defined)\n", hostname)
					} else {
						common.LogInfo("Skipping revert (no revert defined for module/params)", logData)
					}
					continue
				}

				closure := ConstructClosure(hostCtx, task)
				revertResult := task.RevertModule(closure) // Calls module's Revert via Task.RevertModule

				if revertResult.Error != nil {
					logData["status"] = "failed"
					logData["error"] = revertResult.Error.Error()
					if revertResult.Output != nil {
						logData["output"] = revertResult.Output.String()
					}
					recapStats[hostname]["failed"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("failed: [%s] => (%v)\n", hostname, revertResult.Error)
						PPrintOutput(revertResult.Output, revertResult.Error)
					} else {
						common.LogError("Revert task failed", logData)
					}
				} else if revertResult.Output != nil && revertResult.Output.Changed() {
					logData["status"] = "changed"
					logData["changed"] = true
					if revertResult.Output != nil {
						logData["output"] = revertResult.Output.String()
					}
					recapStats[hostname]["changed"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("changed: [%s] => \n%v\n", hostname, revertResult.Output)
					} else {
						common.LogInfo("Revert task changed", logData)
					}
				} else { // OK
					logData["status"] = "ok"
					logData["changed"] = false
					if revertResult.Output != nil {
						logData["output"] = revertResult.Output.String()
					}
					recapStats[hostname]["ok"]++
					if cfg.Logging.Format == "plain" {
						fmt.Printf("ok: [%s]\n", hostname)
						PPrintOutput(revertResult.Output, nil)
					} else {
						common.LogInfo("Revert task ok", logData)
					}
				}
			}
			common.DebugOutput("Finished reverting host '%s' on level %d", hostname, executionLevel)
		}
		common.DebugOutput("Finished reverting level %d", executionLevel)
	}

	if cfg.Logging.Format == "plain" {
		fmt.Printf("\nREVERT RECAP ****************************************************\n")
		for hostname, stats := range recapStats {
			fmt.Printf("%s : ok=%d    changed=%d    failed=%d\n", hostname, stats["ok"], stats["changed"], stats["failed"])
		}
	} else {
		common.LogInfo("Revert recap", map[string]interface{}{"stats": recapStats})
	}

	for _, stats := range recapStats {
		if stats["failed"] > 0 {
			return fmt.Errorf("one or more tasks failed to revert")
		}
	}
	common.DebugOutput("Revert process completed successfully.")
	return nil
}

// GetFirstAvailableHost returns the first host from the hostContexts map
// Used for run_once tasks to select the execution host
func GetFirstAvailableHost(hostContexts map[string]*HostContext) (*HostContext, string) {
	for hostName, hostCtx := range hostContexts {
		return hostCtx, hostName
	}
	return nil, ""
}

// CreateRunOnceResultsForAllHosts creates TaskResult copies for all hosts when run_once is used
// This ensures that facts and results are propagated to all hosts in the batch
func CreateRunOnceResultsForAllHosts(originalResult TaskResult, hostContexts map[string]*HostContext, executionHostName string) []TaskResult {
	var results []TaskResult

	for _, hostCtx := range hostContexts {
		// Create a copy of the result for each host
		result := TaskResult{
			Task:                    originalResult.Task,
			Error:                   originalResult.Error,
			Status:                  originalResult.Status,
			Failed:                  originalResult.Failed,
			Changed:                 originalResult.Changed,
			Duration:                originalResult.Duration,
			Output:                  originalResult.Output,
			ExecutionSpecificOutput: originalResult.ExecutionSpecificOutput,
		}

		// Create a new closure for each host with their own context
		result.Closure = &Closure{
			HostContext: hostCtx,
			ExtraFacts:  make(map[string]interface{}),
		}

		// Copy extra facts from the original closure
		if originalResult.Closure != nil {
			for k, v := range originalResult.Closure.ExtraFacts {
				result.Closure.ExtraFacts[k] = v
			}
		}

		results = append(results, result)
	}

	return results
}
