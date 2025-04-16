package pkg

import (
	"fmt"
	"time"

	"github.com/flosch/pongo2"
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
		templatedWhen, err := TemplateString(t.When, c.Facts)
		pythonResult := pongo2.AsValue(templatedWhen)
		DebugOutput("Evaluating when condition %q: %q, %v", t.When, templatedWhen, pythonResult.IsTrue())
		if err != nil {
			DebugOutput("Error evaluating when condition %q: %s", t.When, err)
			return false
		}
		// TODO: this is a hack to handle the fact that pongo2 returns a python boolean as a string
		if pythonResult.String() == "False" || pythonResult.String() == "None" {
			return false
		}
		return pythonResult.IsTrue()
	}
	return true
}

func (t Task) ExecuteModule(c *HostContext) TaskResult {
	startTime := time.Now()
	r := TaskResult{Task: t, Context: c}

	if !t.ShouldExecute(c) {
		LogDebug("Skipping execution of task", map[string]interface{}{
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
	r := TaskResult{Task: t, Context: c}
	if !t.ShouldExecute(c) {
		LogDebug("Skipping revert of task", map[string]interface{}{
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
	previous := c.History[t.Name]
	r.Output, r.Error = module.Revert(t.Params, c, previous, t.RunAs)
	return r
}
