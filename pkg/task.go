package pkg

import (
	"fmt"

	"github.com/AlexanderGrooff/reconcile/modules"
)

type Task struct {
	Name     string              `yaml:"name"`
	Module   string              `yaml:"module"`
	Params   modules.ModuleInput `yaml:"params"`
	Validate string              `yaml:"validate"`
	Before   string              `yaml:"before"`
	After    string              `yaml:"after"`
	When     string              `yaml:"when"`
	Register string              `yaml:"register"`
}

func (t Task) String() string {
	return fmt.Sprintf("%s (module: %s)", t.Name, t.Module)
}

func (t Task) ExecuteModule() (modules.ModuleOutput, error) {
	module, ok := modules.GetModule(t.Module)
	if !ok {
		return nil, fmt.Errorf("module %s not found", t.Module)
	}
	return module.Execute(t.Params)
}

func (t Task) RevertModule() (modules.ModuleOutput, error) {
	module, ok := modules.GetModule(t.Module)
	if !ok {
		return nil, fmt.Errorf("module %s not found", t.Module)
	}
	return module.Revert(t.Params)
}
