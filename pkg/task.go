package pkg

import (
	"fmt"
)

// Useful for having a single type to pass around in channels
type TaskResult struct {
	Output  ModuleOutput
	Error   error
	Context *HostContext
	Task    Task
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

func (t Task) ToCode(ident int) string {
	return fmt.Sprintf("%s{Name: %q, Module: %q, Register: %q, Params: %s, RunAs: %q},\n",
		Indent(ident),
		t.Name,
		t.Module,
		t.Register,
		t.Params.ToCode(),
		t.RunAs,
	)
}

func (t Task) String() string {
	return t.Name
}

func (t Task) ExecuteModule(c *HostContext) TaskResult {
	r := TaskResult{Task: t, Context: c}
	DebugOutput("Starting task %q on %q", t.Name, c.Host)
	module, ok := GetModule(t.Module)
	if !ok {
		r.Error = fmt.Errorf("module %s not found", t.Module)
		return r
	}
	r.Output, r.Error = module.Execute(t.Params, c, t.RunAs)
	DebugOutput("Completed task %q on %q", t.Name, c.Host)
	return r
}

func (t Task) RevertModule(c *HostContext) TaskResult {
	r := TaskResult{Task: t, Context: c}
	fmt.Printf("[%s - %s]:revert\n", c.Host, t.Name)
	module, ok := GetModule(t.Module)
	if !ok {
		r.Error = fmt.Errorf("module %s not found", t.Module)
		return r
	}
	previous := c.History[t.Name]
	r.Output, r.Error = module.Revert(t.Params, c, previous, t.RunAs)
	return r
}

func (t Task) GetVariableUsage() []string {
	usedVars := []string{}

	return usedVars
}
