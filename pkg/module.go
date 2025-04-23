package pkg

import (
	"fmt"
	"reflect"
)

// ModuleInput is a marker interface for module input parameters.
// It ensures that module parameters have a method to generate their code representation.
type ModuleInput interface {
	ToCode() string
	GetVariableUsage() []string
	Validate() error
}

// ModuleOutput is a marker interface for module output results.
// It ensures that module results can indicate whether they represent a change.
type ModuleOutput interface {
	Changed() bool
	String() string
}

type RevertableChange[T comparable] struct {
	Before T
	After  T
}

func (r RevertableChange[T]) Changed() bool {
	return r.Before != r.After
}

type Module interface {
	InputType() reflect.Type
	OutputType() reflect.Type
	Execute(params ModuleInput, c *HostContext, runAs string) (ModuleOutput, error)
	Revert(params ModuleInput, c *HostContext, previous ModuleOutput, runAs string) (ModuleOutput, error)

	// ParameterAliases returns a map of alias names to canonical parameter names.
	// Example: {"dest": "path", "name": "path"}
	// Modules can return nil or an empty map if they don't have aliases.
	ParameterAliases() map[string]string
}

var registeredModules = make(map[string]Module)

// RegisterModule allows modules to register themselves by name.
func RegisterModule(name string, module Module) {
	if _, exists := registeredModules[name]; exists {
		panic(fmt.Sprintf("Module %s already registered", name))
	}
	registeredModules[name] = module
}

// GetModule retrieves a registered module by name.
func GetModule(name string) (Module, bool) {
	module, ok := registeredModules[name]
	return module, ok
}
