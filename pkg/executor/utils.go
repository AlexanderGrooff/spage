package executor

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/AlexanderGrooff/spage/pkg"
)

func InitializeRecapStats(hostContexts map[string]*pkg.HostContext) map[string]map[string]int {
	recapStats := make(map[string]map[string]int)
	for hostname := range hostContexts {
		recapStats[hostname] = map[string]int{"ok": 0, "changed": 0, "failed": 0, "skipped": 0, "ignored": 0}
	}
	return recapStats
}

func CalculateExpectedResults(
	tasksInLevel []pkg.Task,
	hostContexts map[string]*pkg.HostContext,
) (int, error) {
	numExpectedResultsOnLevel := 0
	for _, task := range tasksInLevel {
		// Special handling for run_once, as it executes on one host but produces results for all.
		if task.RunOnce {
			if len(hostContexts) > 0 {
				// One execution (or one aggregated execution for loops) produces a result for every host.
				numExpectedResultsOnLevel += len(hostContexts)
			}
			continue
		}

		// For regular tasks, count executions across all hosts.
		for _, hc := range hostContexts {
			// Create a temporary closure for delegate_to resolution.
			// The actual host context used for closure calculation depends on delegate_to.
			resolutionClosure := pkg.ConstructClosure(hc, task)
			effectiveHostCtx, err := GetDelegatedHostContext(task, hostContexts, resolutionClosure)
			if err != nil {
				return 0, fmt.Errorf("failed to resolve delegate_to for count on task '%s', host '%s': %w", task.Name, hc.Host.Name, err)
			}
			if effectiveHostCtx == nil {
				effectiveHostCtx = hc // No delegation, use original host context.
			}

			closures, err := GetTaskClosures(task, effectiveHostCtx)
			if err != nil {
				return 0, fmt.Errorf("failed to get task closures for count on task '%s', host '%s': %w", task.Name, effectiveHostCtx.Host.Name, err)
			}
			numExpectedResultsOnLevel += len(closures)
		}
	}
	return numExpectedResultsOnLevel, nil
}

func PrepareLevelHistoryAndGetCount(
	tasksInLevel []pkg.Task,
	hostContexts map[string]*pkg.HostContext,
	executionLevel int,
) (map[string]chan pkg.Task, int, error) {
	levelHistoryForRevert := make(map[string]chan pkg.Task)
	for hostname := range hostContexts {
		// Buffer size can be a best-effort guess; it doesn't need to be perfect for local execution.
		// A safer approach might be to calculate per-host task count, but total count is simpler.
		levelHistoryForRevert[hostname] = make(chan pkg.Task, len(tasksInLevel)*2) // Simple heuristic
	}

	numExpectedResultsOnLevel, err := CalculateExpectedResults(tasksInLevel, hostContexts)
	if err != nil {
		return nil, 0, err
	}

	return levelHistoryForRevert, numExpectedResultsOnLevel, nil
}

// GetFirstAvailableHost returns the first host from the hostContexts map
// Used for run_once tasks to select the execution host. It sorts the hosts
// by name to ensure deterministic execution.
func GetFirstAvailableHost(hostContexts map[string]*pkg.HostContext) (*pkg.HostContext, string) {
	if len(hostContexts) == 0 {
		return nil, ""
	}

	// Get host names and sort them to ensure consistent ordering
	hostNames := make([]string, 0, len(hostContexts))
	for hostName := range hostContexts {
		hostNames = append(hostNames, hostName)
	}
	sort.Strings(hostNames)

	// Return the first host context based on sorted order
	firstName := hostNames[0]
	return hostContexts[firstName], firstName
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
				} else {
					// Fall through to string templating logic below
				}
			} else {
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
		}

	case []interface{}:
		loopItems = loopValue

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
