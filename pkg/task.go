package pkg

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"
	// Remove pongo2 import if no longer needed directly here
	// "github.com/flosch/pongo2"
)

// Useful for having a single type to pass around in channels
type TaskResult struct {
	Output   ModuleOutput
	Error    error
	Context  *HostContext
	Task     Task
	Duration time.Duration
}

type Task struct {
	Name     string      `yaml:"name"`
	Module   string      `yaml:"module"`
	Params   ModuleInput `yaml:"params"`
	Validate string      `yaml:"validate"`
	Before   string      `yaml:"before"`
	After    string      `yaml:"after"`
	When     string      `yaml:"when"`
	Register string      `yaml:"register"`
	RunAs    string      `yaml:"run_as"`
}

func (t Task) ToCode() string {
	return fmt.Sprintf("pkg.Task{Name: %q, Module: %q, Register: %q, Params: %s, RunAs: %q, When: %q},\n",
		t.Name,
		t.Module,
		t.Register,
		t.Params.ToCode(),
		t.RunAs,
		t.When,
	)
}

func (t Task) String() string {
	return t.Name
}

func (t Task) ShouldExecute(c *HostContext) bool {
	if t.When != "" {
		// Template the 'when' condition string using available facts and history
		templatedWhen, err := TemplateString(t.When, c.Facts, c.History)
		if err != nil {
			// If templating fails, we cannot evaluate the condition, so skip the task
			common.LogWarn("Error templating when condition, skipping task", map[string]interface{}{
				"task":      t.Name,
				"host":      c.Host.Name,
				"condition": t.When,
				"error":     err.Error(),
			})
			return false
		}

		// Trim whitespace and parse the resulting string as a boolean
		trimmedResult := strings.TrimSpace(templatedWhen)
		result, err := strconv.ParseBool(trimmedResult)

		// Log the evaluation result
		common.DebugOutput("Evaluated when condition %q -> %q: %t (error: %v)",
			t.When, trimmedResult, result, err)

		if err != nil {
			// If the result isn't a valid boolean string, treat it as false (skip task)
			common.LogWarn("When condition did not evaluate to a boolean value, skipping task", map[string]interface{}{
				"task":      t.Name,
				"host":      c.Host.Name,
				"condition": t.When,
				"evaluated": trimmedResult,
			})
			return false
		}

		// Return the parsed boolean result
		return result
	}
	// If no 'when' condition, always execute
	return true
}

func (t Task) ExecuteModule(c *HostContext) TaskResult {
	startTime := time.Now()
	r := TaskResult{Task: t, Context: c}

	if !t.ShouldExecute(c) {
		common.LogDebug("Skipping execution of task", map[string]interface{}{
			"task": t.Name,
			"host": c.Host.Name,
		})
		return r
	}

	module, ok := GetModule(t.Module)
	if !ok {
		r.Error = fmt.Errorf("module %s not found", t.Module)
		return r
	}

	r.Output, r.Error = module.Execute(t.Params, c, t.RunAs)
	duration := time.Since(startTime)
	r.Duration = duration

	return r
}

func (t Task) RevertModule(c *HostContext) TaskResult {
	startTime := time.Now()
	r := TaskResult{Task: t, Context: c}
	if !t.ShouldExecute(c) {
		common.LogDebug("Skipping revert of task", map[string]interface{}{
			"task": t.Name,
			"host": c.Host.Name,
		})
		return r
	}
	module, ok := GetModule(t.Module)
	if !ok {
		r.Error = fmt.Errorf("module %s not found", t.Module)
		return r
	}

	// Load previous output from history using sync.Map.Load
	previousOutputRaw, found := c.History.Load(t.Name) // Changed from index access
	if !found {
		common.LogWarn("No previous history found for task during revert", map[string]interface{}{
			"task": t.Name,
			"host": c.Host.Name,
		})
		previousOutputRaw = nil
	}

	// Type assert the loaded value to ModuleOutput
	var previousOutput ModuleOutput
	if previousOutputRaw != nil {
		var assertOk bool
		previousOutput, assertOk = previousOutputRaw.(ModuleOutput)
		if !assertOk {
			r.Error = fmt.Errorf("failed to assert previous history type (%T) to ModuleOutput for task %s", previousOutputRaw, t.Name)
			r.Duration = time.Since(startTime)
			return r
		}
	}

	// Execute the revert logic of the module
	r.Output, r.Error = module.Revert(t.Params, c, previousOutput, t.RunAs) // Pass potentially nil previousOutput
	duration := time.Since(startTime)
	r.Duration = duration

	return r
}
