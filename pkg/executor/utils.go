package executor

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
	"github.com/AlexanderGrooff/spage/pkg/common"
)

func InitializeRecapStats(hostContexts map[string]*pkg.HostContext) map[string]map[string]int {
	recapStats := make(map[string]map[string]int)
	for hostname := range hostContexts {
		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0, "skipped": 0, "ignored": 0}
	}
	return recapStats
}

func PrepareLevelHistoryAndGetCount(
	tasksInLevel []pkg.Task,
	hostContexts map[string]*pkg.HostContext,
	executionLevel int,
) (map[string]chan pkg.Task, int, error) {
	levelHistoryForRevert := make(map[string]chan pkg.Task)
	numExpectedResultsOnLevel := 0

	for hostname, hc := range hostContexts {
		numTasksForHostOnLevel := 0
		for _, task := range tasksInLevel {
			// Create a temporary closure for delegate_to resolution
			tempClosure := pkg.ConstructClosure(hc, task)
			delegatedHostContext, err := GetDelegatedHostContext(task, hostContexts, tempClosure)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to get host for task: %w", err)
			}
			if delegatedHostContext != nil {
				hc = delegatedHostContext
			}

			// For run_once tasks, we still need to count them for each host
			// because we'll send results to all hosts, but we only execute once
			if task.RunOnce {
				// For run_once tasks, count them as 1 per host (for result propagation)
				// but we'll only execute on the first host
				closures, err := GetTaskClosures(task, hc)
				if err != nil {
					return nil, 0, fmt.Errorf("failed to get task closures for count: %w", err)
				}
				numTasksForHostOnLevel += len(closures)
			} else {
				// Normal task counting
				closures, err := GetTaskClosures(task, hc)
				if err != nil {
					return nil, 0, fmt.Errorf("failed to get task closures for count: %w", err)
				}
				numTasksForHostOnLevel += len(closures)
			}
		}
		levelHistoryForRevert[hostname] = make(chan pkg.Task, numTasksForHostOnLevel)
		numExpectedResultsOnLevel += numTasksForHostOnLevel
		common.DebugOutput("Expecting %d task instances for host '%s' on level %d", numTasksForHostOnLevel, hostname, executionLevel)
	}
	return levelHistoryForRevert, numExpectedResultsOnLevel, nil
}

// GetFirstAvailableHost returns the first host from the hostContexts map
// Used for run_once tasks to select the execution host
func GetFirstAvailableHost(hostContexts map[string]*pkg.HostContext) (*pkg.HostContext, string) {
	for hostName, hostCtx := range hostContexts {
		return hostCtx, hostName
	}
	return nil, ""
}

// GetTaskClosures generates one or more Closures for a task, handling loops.
func GetTaskClosures(task pkg.Task, c *pkg.HostContext) ([]*pkg.Closure, error) {
	if task.Loop == nil {
		closure := pkg.ConstructClosure(c, task) // Assumes ConstructClosure is available
		return []*pkg.Closure{closure}, nil
	}

	loopItems, err := ParseLoop(task, c)
	if err != nil {
		return nil, fmt.Errorf("failed to parse loop for task '%s' on host '%s': %w", task.Name, c.Host.Name, err)
	}

	var closures []*pkg.Closure
	for _, item := range loopItems {
		tClosure := pkg.ConstructClosure(c, task)
		// "item" is the standard variable name for loop items.
		// Ansible's loop_control.loop_var allows customization; this is not currently supported here.
		tClosure.ExtraFacts["item"] = item
		closures = append(closures, tClosure)
	}
	return closures, nil
}

// ParseLoop parses the loop directive of a task, evaluating expressions and returning items to iterate over.
func ParseLoop(task pkg.Task, c *pkg.HostContext) ([]interface{}, error) {
	var loopItems []interface{}
	closure := pkg.ConstructClosure(c, task) // Assumes ConstructClosure is available from context.go or similar

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
			evalLoopStr, err := pkg.TemplateString(loopValue, closure) // Assumes TemplateString is available from context.go or similar
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

func GetDelegatedHostContext(task pkg.Task, hostContexts map[string]*pkg.HostContext, closure *pkg.Closure) (*pkg.HostContext, error) {
	if task.DelegateTo != "" {
		// Template the delegate_to value in case it contains Jinja variables
		delegateTo, err := pkg.TemplateString(task.DelegateTo, closure)
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
			localhostHost := &pkg.Host{
				Name:    "localhost",
				Host:    "localhost",
				IsLocal: true,
				Vars:    make(map[string]interface{}),
				Groups:  make(map[string]string),
			}

			// Initialize host context for localhost
			localhostContext, err := pkg.InitializeHostContext(localhostHost)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize context for localhost: %w", err)
			}

			// Copy all facts from the original closure's context to the localhost context
			// This ensures that variables set in previous tasks are available
			closure.HostContext.Facts.Range(func(key, value interface{}) bool {
				localhostContext.Facts.Store(key, value)
				return true
			})

			// Add basic localhost facts (override any existing ones)
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

// CreateRunOnceResultsForAllHosts creates TaskResult copies for all hosts when run_once is used
// This ensures that facts and results are propagated to all hosts in the batch
func CreateRunOnceResultsForAllHosts(originalResult pkg.TaskResult, hostContexts map[string]*pkg.HostContext, executionHostName string) []pkg.TaskResult {
	var results []pkg.TaskResult

	for _, hostCtx := range hostContexts {
		// Create a copy of the result for each host
		result := pkg.TaskResult{
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
		result.Closure = &pkg.Closure{
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

// PPrintOutput prints the output of a module or an error in a readable format.
func PPrintOutput(output pkg.ModuleOutput, err error) {
	if output != nil {
		// Attempt to get a string representation. Assumes ModuleOutput implements fmt.Stringer or can be printed directly.
		// More sophisticated printing might involve checking specific fields or using a custom Marshal-like method.
		fmt.Printf("%v\n", output)
	} else if err != nil {
		// Error is usually printed by the caller context, so no duplicate print here.
		// fmt.Printf("Error: %v\n", err) // Avoid double printing of error message
	}
}
