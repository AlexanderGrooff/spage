package pkg

import (
	"fmt"
)

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

func (t Task) ExecuteModule(c HostContext) (ModuleOutput, error) {
	module, ok := GetModule(t.Module)
	if !ok {
		return nil, fmt.Errorf("module %s not found", t.Module)
	}
	output, err := module.Execute(t.Params, c)
	moduleOutput, ok := output.(ModuleOutput)
	if !ok {
		return moduleOutput, fmt.Errorf("module %s did not return a valid ModuleOutput: %s", t.Module, err)
	}
	return moduleOutput, err
}

func (t Task) RevertModule(c HostContext) (ModuleOutput, error) {
	module, ok := GetModule(t.Module)
	if !ok {
		return nil, fmt.Errorf("module %s not found", t.Module)
	}
	previous := c.History[t.Name]
	output, err := module.Revert(t.Params, c, previous)
	if err != nil {
		return nil, err
	}
	moduleOutput, ok := output.(ModuleOutput)
	if !ok {
		return nil, fmt.Errorf("module %s did not return a valid ModuleOutput", t.Module)
	}
	return moduleOutput, nil
}

func (t Task) GetVariableUsage() []string {
	usedVars := []string{}
		

	return usedVars
}
