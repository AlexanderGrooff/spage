package pkg

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// ModuleInput is a marker interface for module input parameters.
// It ensures that module parameters have a method to generate their code representation.
type ConcreteModuleInputProvider interface {
	ToCode() string
	GetVariableUsage() []string
	Validate() error
	// HasRevert indicates if this input defines a revert action.
	HasRevert() bool
	// ProvidesVariables returns a list of variable names this input might define (e.g., keys in set_fact).
	ProvidesVariables() []string
}

// ModuleInput is now a struct that wraps the actual module parameters.
// Its primary role is to facilitate correct JSON/YAML marshaling/unmarshaling
// when it's a field within another struct (like Task).
type ModuleInput struct {
	// Actual holds the concrete *ShellInput, *CopyInput, etc.
	// This field is populated by custom UnmarshalJSON/YAML methods on the Task struct.
	Actual ConcreteModuleInputProvider `json:"-" yaml:"-"`
}

// MarshalJSON implements the json.Marshaler interface for ModuleInput.
// It ensures that the Actual ConcreteModuleInputProvider is serialized.
func (mi *ModuleInput) MarshalJSON() ([]byte, error) {
	if mi.Actual != nil {
		return json.Marshal(mi.Actual)
	}
	return json.Marshal(nil) // Or use []byte("{}") for an empty JSON object
}

// MarshalYAML implements the yaml.Marshaler interface for ModuleInput.
// It ensures that the Actual ConcreteModuleInputProvider is serialized.
func (mi *ModuleInput) MarshalYAML() (interface{}, error) {
	if mi.Actual != nil {
		return mi.Actual, nil
	}
	return nil, nil
}

// ToCode delegates to the Actual ConcreteModuleInputProvider.
// It's called during code generation, Actual must be populated.
func (mi *ModuleInput) ToCode() string {
	if mi.Actual == nil {
		// This indicates a programming error if called before Actual is populated.
		panic("ModuleInput.Actual is nil when ToCode() is called")
	}
	return mi.Actual.ToCode()
}

// GetVariableUsage delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) GetVariableUsage() []string {
	if mi.Actual == nil {
		panic("ModuleInput.Actual is nil when GetVariableUsage() is called")
	}
	return mi.Actual.GetVariableUsage()
}

// Validate delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) Validate() error {
	if mi.Actual == nil {
		panic("ModuleInput.Actual is nil when Validate() is called")
	}
	return mi.Actual.Validate()
}

// HasRevert delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) HasRevert() bool {
	if mi.Actual == nil {
		// Default to false if Actual is not populated, or panic as above.
		// For safety, returning false might be less disruptive than panicking here.
		return false
	}
	return mi.Actual.HasRevert()
}

// ProvidesVariables delegates to the Actual ConcreteModuleInputProvider.
func (mi *ModuleInput) ProvidesVariables() []string {
	if mi.Actual == nil {
		// Default to nil or empty slice.
		return nil
	}
	return mi.Actual.ProvidesVariables()
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
	Execute(params ConcreteModuleInputProvider, c *Closure, runAs string) (ModuleOutput, error)
	Revert(params ConcreteModuleInputProvider, c *Closure, previous ModuleOutput, runAs string) (ModuleOutput, error)
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
