package pkg

import "reflect"

type ModuleInput interface {
	ToCode() string
	GetVariableUsage() []string
	Validate() error
}
type ModuleOutput interface {
	String() string
	Changed() bool
}
type RevertableChange[T comparable] struct {
	Before T
	After  T
}

func (r RevertableChange[T]) Changed() bool {
	return r.Before != r.After
}

type Module interface {
	Execute(params ModuleInput, c *HostContext, runAs string) (ModuleOutput, error)
	Revert(params ModuleInput, c *HostContext, previous ModuleOutput, runAs string) (ModuleOutput, error)
	InputType() reflect.Type
	OutputType() reflect.Type
}

// ModuleRegistry stores available modules
var ModuleRegistry = make(map[string]Module)

func RegisterModule(name string, module Module) {
	ModuleRegistry[name] = module
}

func GetModule(name string) (Module, bool) {
	module, ok := ModuleRegistry[name]
	return module, ok
}
