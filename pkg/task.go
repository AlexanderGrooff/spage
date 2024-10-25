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
	return fmt.Sprintf("%s{Name: %q, Module: %q, Params: %s},\n",
		Indent(ident),
		t.Name,
		t.Module,
		t.Params.ToCode(ident+1),
	)
}

func (t Task) String() string {
	return fmt.Sprintf("%s (module: %s)", t.Name, t.Module)
}

func (t Task) ExecuteModule(c Context) (ModuleOutput, error) {
	module, ok := GetModule(t.Module)
	if !ok {
		return nil, fmt.Errorf("module %s not found", t.Module)
	}
	return module.Execute(t.Params, c)
}

func (t Task) RevertModule(c Context) (ModuleOutput, error) {
	module, ok := GetModule(t.Module)
	if !ok {
		return nil, fmt.Errorf("module %s not found", t.Module)
	}
	return module.Revert(t.Params, c)
}
