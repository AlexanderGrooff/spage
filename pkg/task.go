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
}

func (t Task) ToCode(ident int) string {
	return fmt.Sprintf("%s{Name: %q, Module: %q, Register: %q, Params: %s,},\n",
		Indent(ident),
		t.Name,
		t.Module,
		t.Register,
		t.Params.ToCode(),
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
	output, err := module.Execute(t.Params, c)
	r.Error = err
	moduleOutput, ok := output.(ModuleOutput)
	if !ok {
		r.Error = fmt.Errorf("module %s did not return a valid ModuleOutput", t.Module)
	} else {
		r.Output = moduleOutput
	}
	DebugOutput("Completed task %q on %q", t.Name, c.Host)
	return r
}

func (t Task) RevertModule(c *HostContext) TaskResult {
	r := TaskResult{Task: t, Context: c}
	fmt.Printf("[%s - %s]:revert\n", c.Host.Host, t.Name)
	module, ok := GetModule(t.Module)
	if !ok {
		r.Error = fmt.Errorf("module %s not found", t.Module)
		return r
	}
	previous := c.History[t.Name]
	output, err := module.Revert(t.Params, c, previous)
	r.Error = err
	moduleOutput, ok := output.(ModuleOutput)
	if !ok {
		r.Error = fmt.Errorf("module %s did not return a valid ModuleOutput", t.Module)
	} else {
		r.Output = moduleOutput
	}
	return r
}

func (t Task) GetVariableUsage() []string {
	usedVars := []string{}

	return usedVars
}
