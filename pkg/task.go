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
	return fmt.Sprintf("%s{Name: %q, Module: %q, Register: %q, Params: %s},\n",
		Indent(ident),
		t.Name,
		t.Module,
		t.Register,
		t.Params.ToCode(ident+1),
	)
}

func (t Task) String() string {
	return t.Name
}

func (t Task) ExecuteModule(c Context) (ModuleOutput, error) {
	module, ok := GetModule(t.Module)
	if !ok {
		return nil, fmt.Errorf("module %s not found", t.Module)
	}
	output, err := module.Execute(t.Params, c)
	if err != nil {
		return nil, err
	}
	moduleOutput, ok := output.(ModuleOutput)
	if !ok {
		return moduleOutput, fmt.Errorf("module %s did not return a valid ModuleOutput", t.Module)
	}
	return moduleOutput, nil
}

func (t Task) RevertModule(c Context) (ModuleOutput, error) {
	module, ok := GetModule(t.Module)
	if !ok {
		return nil, fmt.Errorf("module %s not found", t.Module)
	}
	output, err := module.Revert(t.Params, c)
	if err != nil {
		return nil, err
	}
	moduleOutput, ok := output.(ModuleOutput)
	if !ok {
		return nil, fmt.Errorf("module %s did not return a valid ModuleOutput", t.Module)
	}
	return moduleOutput, nil
}
