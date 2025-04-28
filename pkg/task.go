package pkg

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AlexanderGrooff/spage/pkg/common"
	// Remove pongo2 import if no longer needed directly here
	// "github.com/flosch/pongo2"
)

// Useful for having a single type to pass around in channels
type TaskResult struct {
	Output   ModuleOutput
	Error    error // This can now be nil, a normal error, or an IgnoredTaskError
	Context  *HostContext
	Task     Task
	Duration time.Duration
}

// IgnoredTaskError is a custom error type used when a task fails
// but IgnoreErrors is set to true.
// It wraps the original error.
type IgnoredTaskError struct {
	OriginalErr error
}

// Error implements the error interface for IgnoredTaskError.
func (e *IgnoredTaskError) Error() string {
	if e.OriginalErr != nil {
		return fmt.Sprintf("ignored: %s", e.OriginalErr.Error())
	}
	return "ignored error (no original error specified)"
}

// Unwrap allows errors.Is and errors.As to work with the wrapped original error.
func (e *IgnoredTaskError) Unwrap() error {
	return e.OriginalErr
}

// FactProvider is an interface that module outputs can implement
// to provide a map representation suitable for registering as facts.
type FactProvider interface {
	AsFacts() map[string]interface{}
}

type Task struct {
	Name         string      `yaml:"name"`
	Module       string      `yaml:"module"`
	Params       ModuleInput `yaml:"params"`
	Validate     string      `yaml:"validate"`
	Before       string      `yaml:"before"`
	After        string      `yaml:"after"`
	When         string      `yaml:"when"`
	Register     string      `yaml:"register"`
	RunAs        string      `yaml:"run_as"`
	IgnoreErrors bool        `yaml:"ignore_errors,omitempty"`
	FailedWhen   string      `yaml:"failed_when,omitempty"`
}

func (t Task) ToCode() string {
	return fmt.Sprintf("pkg.Task{Name: %q, Module: %q, Register: %q, Params: %s, RunAs: %q, When: %q, IgnoreErrors: %t, FailedWhen: %q},\n",
		t.Name,
		t.Module,
		t.Register,
		t.Params.ToCode(),
		t.RunAs,
		t.When,
		t.IgnoreErrors,
		t.FailedWhen,
	)
}

func (t Task) String() string {
	return t.Name
}

func (t Task) ShouldExecute(c *HostContext) bool {
	if t.When != "" {
		templatedWhen, err := EvaluateExpression(t.When, c.Facts)
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

		// Evaluate truthiness using the helper function
		conditionMet := IsExpressionTruthy(templatedWhen)
		trimmedResult := strings.TrimSpace(templatedWhen) // Still needed for logging

		// Log the evaluation result
		common.DebugOutput("Evaluated when condition %q -> %q: %t",
			t.When, trimmedResult, conditionMet)

		// Return the evaluated truthiness
		return conditionMet
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

	common.DebugOutput("Executing module %s with params %v and context %v", t.Module, t.Params, c)
	r.Output, r.Error = module.Execute(t.Params, c, t.RunAs)
	duration := time.Since(startTime)
	r.Duration = duration

	// --- Start FailedWhen Check ---
	// Evaluate failed_when only if the module execution itself succeeded
	if r.Error == nil && t.FailedWhen != "" {
		// Convert module output to facts map for evaluation context
		var outputFacts *sync.Map
		if r.Output != nil {
			if factProvider, ok := r.Output.(FactProvider); ok {
				factsMap := factProvider.AsFacts()
				outputFacts = MapToSyncMap(factsMap) // Convert map[string]interface{} to *sync.Map
			} else {
				common.LogWarn("Module output does not implement FactProvider, cannot use in failed_when", map[string]interface{}{
					"task":   t.Name,
					"module": t.Module,
					"output": fmt.Sprintf("%T", r.Output),
				})
			}
		}

		// Evaluate the expression, merging host facts and task output facts
		templatedFailedWhen, err := EvaluateExpression(t.FailedWhen, c.Facts, outputFacts)
		if err != nil {
			// Treat evaluation errors as task failure, as we can't determine the condition
			r.Error = fmt.Errorf("error evaluating failed_when condition '%s': %w", t.FailedWhen, err)
			common.LogWarn("Error evaluating failed_when condition, marking task as failed", map[string]interface{}{
				"task":      t.Name,
				"host":      c.Host.Name,
				"condition": t.FailedWhen,
				"error":     err.Error(),
			})
		} else {
			// Evaluate truthiness of the result
			conditionMet := IsExpressionTruthy(templatedFailedWhen)
			trimmedResult := strings.TrimSpace(templatedFailedWhen)
			common.DebugOutput("Evaluated failed_when condition %q -> %q: %t",
				t.FailedWhen, trimmedResult, conditionMet)

			if conditionMet {
				// Set the error if the condition is true
				r.Error = fmt.Errorf("failed_when condition '%s' evaluated to true (%s)", t.FailedWhen, trimmedResult)
			}
		}
	}
	// --- End FailedWhen Check ---

	// Handle IgnoreErrors - this now runs *after* failed_when might have set an error
	if r.Error != nil && t.IgnoreErrors {
		common.LogWarn("Task failed but error ignored due to ignore_errors=true", map[string]interface{}{
			"task":  t.Name,
			"host":  c.Host.Name,
			"error": r.Error.Error(),
		})
		// Wrap the original error in IgnoredTaskError
		r.Error = &IgnoredTaskError{OriginalErr: r.Error}
	}

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

	// Handle IgnoreErrors for revert
	if r.Error != nil && t.IgnoreErrors {
		common.LogWarn("Revert failed but error ignored due to ignore_errors=true", map[string]interface{}{
			"task":  t.Name,
			"host":  c.Host.Name,
			"error": r.Error.Error(),
		})
		// Wrap the original error in IgnoredTaskError
		r.Error = &IgnoredTaskError{OriginalErr: r.Error}
	}

	return r
}
